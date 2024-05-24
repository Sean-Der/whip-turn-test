package main

import (
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/pion/turn/v3"
	"github.com/pion/webrtc/v4"
)

var (
	api *webrtc.API
)

func doSignaling(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Link", `<turn:172.27.253.31>; rel="ice-server"; username="username"; credential="password";`)
	if r.Method == "OPTIONS" || r.Method == "DELETE" {
		return
	}

	offer, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}

	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		panic(err)
	}

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
		fmt.Printf("Getting incoming media %s\n", track.Codec().MimeType)
	})

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())

		if connectionState == webrtc.ICEConnectionStateFailed {
			peerConnection.Close()
		}
	})

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(offer)}); err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	} else if err = peerConnection.SetLocalDescription(answer); err != nil {
		fmt.Println(answer.SDP)
		panic(err)
	}

	<-gatherComplete

	w.Header().Add("Location", "/")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, peerConnection.LocalDescription().SDP)
}

type turnOnlyUDPConn struct {
	*net.UDPConn
}

func (t *turnOnlyUDPConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = t.UDPConn.ReadFrom(p)
	srcPort := addr.(*net.UDPAddr).Port
	if srcPort < 5000 || srcPort > 5010 {
		return 0, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}, nil
	}

	return
}

func startTURNServer() {
	udpListener, err := net.ListenPacket("udp4", "0.0.0.0:3478")
	if err != nil {
		panic(err)
	}

	_, err = turn.NewServer(turn.ServerConfig{
		Realm: "siobud.com",
		AuthHandler: func(username string, realm string, srcAddr net.Addr) ([]byte, bool) { // nolint: revive
			return turn.GenerateAuthKey("username", "siobud.com", "password"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: net.ParseIP("127.0.0.1"),
					Address:      "0.0.0.0",
					MinPort:      5000,
					MaxPort:      5010,
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
}

func main() {
	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IP{0, 0, 0, 0}, Port: 8000})
	if err != nil {
		panic(err)
	}

	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEUDPMux(webrtc.NewICEUDPMux(nil, &turnOnlyUDPConn{udpListener}))
	api = webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))

	startTURNServer()

	http.HandleFunc("/", doSignaling)

	fmt.Println("Running WHIP server at http://localhost:8085")
	// nolint: gosec
	panic(http.ListenAndServe(":8085", nil))
}
