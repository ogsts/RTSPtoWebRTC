package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
	"os"
	"github.com/deepch/vdk/av"
	"github.com/pion/webrtc/v3"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/pion/webrtc/v3/pkg/media"
)


var (
	ErrorNotFound          = errors.New("WebRTC Stream Not Found")
	ErrorCodecNotSupported = errors.New("WebRTC Codec Not Supported")
	ErrorClientOffline     = errors.New("WebRTC Client Offline")
	ErrorNotTrackAvailable = errors.New("WebRTC Not Track Available")
	ErrorIgnoreAudioTrack  = errors.New("WebRTC Ignore Audio Track codec not supported WebRTC support only PCM_ALAW or PCM_MULAW")
)


type JCodec struct {
	Type string
}

type Msg struct {
	Type* string
	Sdp* string
	Candidate* string
	SdpMid* string
	SdpMLineIndex* uint16
	UsernameFragment* string
	Param* string
}

type Candidate struct {
	Candidate *webrtc.ICECandidate  `json:candidate`
}

type Muxer struct {
	ws            *websocket.Conn
	streams       map[int8]*Stream
	status        webrtc.ICEConnectionState
	Stop          bool
	pc            *webrtc.PeerConnection
	ClientACK     *time.Timer
	StreamACK     *time.Timer
	Options       Options
	rtpSenders    map[int8]*webrtc.RTPSender
}

type Stream struct {
	codec av.CodecData
	track *webrtc.TrackLocalStaticSample
}


type Options struct {
	ICEServers []string
	ICEUsername string
	ICECredential string
	PortMin uint16
	PortMax uint16
}

func handleConnection(ws *websocket.Conn) {
	
	
	mux := NewMuxer(Options{
			ICEServers: Config.GetICEServers(),
			ICEUsername: Config.GetICEUsername(),
			ICECredential: Config.GetICECredential(),
			PortMin: Config.GetWebRTCPortMin(),
			PortMax: Config.GetWebRTCPortMax()}, ws)
	
	err := mux.startPeerConnection()
	if err != nil {
		log.Println(err)
		return
	}
	
	for {
		if (mux.Stop) {
			break
		}
		
		_, data, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}

		fmt.Println("Message: ", string(data))
		
		var msg Msg
		json.Unmarshal([]byte(data), &msg)


		if(msg.Type != nil){
			fmt.Println("Type: ", *msg.Type )		
		}
		if(msg.Sdp != nil){
			fmt.Println("Sdp: ", *msg.Sdp )		
		}
		if(msg.Candidate != nil){
			fmt.Println("Candidate: ", *msg.Candidate )		
		}
		if(msg.SdpMid != nil){
			fmt.Println("sdpMid: ", *msg.SdpMid )
		}
		if(msg.SdpMLineIndex != nil){
			fmt.Println("sdpMLineIndex: ", *msg.SdpMLineIndex )
		}
		if(msg.UsernameFragment != nil){
			fmt.Println("usernameFragment: ", *msg.UsernameFragment )
		}
		if(msg.Param != nil){
			fmt.Println("params: ", *msg.Param)
		}

		if(msg.Type != nil) {
			
			if(*msg.Type == "offer" && msg.Sdp != nil) {
				log.Println("	reader() OFFER")
				mux.handleOffer(*msg.Sdp)
			}
			if(*msg.Type == "answer" && msg.Sdp != nil) {
				log.Println("	reader() ANSWER")
				mux.handleAnswer(*msg.Sdp)
			}
			if(*msg.Type == "addTrack" && msg.Param != nil) {
				log.Println("	reader() ADDTRACK")
				mux.handleAddTrack(*msg.Param)
			}
		}
		
		if (msg.Candidate != nil) {
			log.Println("	reader() CANDIDATE")
			mux.handleCandidate(*msg.Candidate, msg.SdpMid, msg.SdpMLineIndex, msg.UsernameFragment)
		}
		
	} 
}

func NewMuxer(options Options, conn *websocket.Conn) *Muxer {
	
	log.Println("#-> NewMuxer")
	
	tmp := Muxer {
		Options: options, 
		ClientACK: time.NewTimer(time.Second * 20),
		StreamACK: time.NewTimer(time.Second * 20),
		streams: make(map[int8]*Stream),
		rtpSenders: make(map[int8]*webrtc.RTPSender),
		ws: conn }
	
	log.Println("<-# NewMuxer")
	return &tmp
}

func (this *Muxer) handleCandidate(sdp string, sdpmid* string, sdplineindex* uint16, userfragment* string) (error) {
	
	log.Println("#-> Muxer::handleCandidate")
	
	if err := this.pc.AddICECandidate(webrtc.ICECandidateInit{Candidate: sdp,SDPMid: sdpmid, SDPMLineIndex: sdplineindex, UsernameFragment: userfragment }); err != nil {
		log.Println(err)
		return err
	}
	
	log.Println("<-# Muxer::handleCandidate OK")
	return nil
}

func (this *Muxer) handleAnswer(sdp string) (error) {
	
	log.Println("#-> Muxer::handleAnswer")
	
	var answer webrtc.SessionDescription
	answer.Type = webrtc.SDPTypeAnswer
	answer.SDP = sdp
	
	if err := this.pc.SetRemoteDescription(answer); err != nil {
		log.Println("<-# Muxer::handleAnswer FAILED!")
		return err
	}
	log.Println("<-# Muxer::handleAnswer OK")
	return nil
}

func (this *Muxer) handleOffer(sdp string) (error) {

	log.Println("#-> Muxer::handleOffer() " , this.pc.SignalingState().String())

	if (this.pc.SignalingState().String() != "stable") {
		log.Println("	Muxer::handleOffer() Cant process offer in this state: ",this.pc.SignalingState().String())
		return nil
	}
	
	var offer webrtc.SessionDescription
	offer.Type = webrtc.SDPTypeOffer
	offer.SDP = sdp

	var err error = nil
	if err = this.pc.SetRemoteDescription(offer); err != nil {
		return err
	}
	
	log.Println("	Muxer::handleOffer() SetRemoteDescription OK")

	answer, err := this.pc.CreateAnswer(nil)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Println("	Muxer::handleOffer() Answer OK")

	if err = this.pc.SetLocalDescription(answer); err != nil {
		log.Println(err)
		return err
	}
	log.Println("	Muxer::handleOffer() SetLocalDescription OK")

	response, err := json.Marshal(*this.pc.LocalDescription())
	if err != nil {
		log.Println(err)
		return err
	}

	log.Println("	Muxer::handleOffer() marshal OK")
	if err = this.ws.WriteMessage(1, response); err != nil {
		log.Println(err)
		return err
	}

	log.Println("<-# Muxer::handleOffer() write OK")
	return nil

}

func (this *Muxer) NewPeerConnection(configuration webrtc.Configuration) (*webrtc.PeerConnection, error) {
	
	log.Println("#-> NewPeerConnection")
	
	if len(this.Options.ICEServers) > 0 {
		
		log.Println("Set ICEServers", this.Options.ICEServers)
		
		configuration.ICEServers = append(configuration.ICEServers, webrtc.ICEServer{
			URLs:           this.Options.ICEServers,
			Username:       this.Options.ICEUsername,
			Credential:     this.Options.ICECredential,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}
	
	
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	
	
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}
	
	
	s := webrtc.SettingEngine{}
	
	if (this.Options.PortMin > 0 && this.Options.PortMax > 0 && this.Options.PortMax > this.Options.PortMin) {
		
		
		s.SetEphemeralUDPPortRange(this.Options.PortMin, this.Options.PortMax)
		
		log.Println("Set UDP ports to", this.Options.PortMin, "..", this.Options.PortMax)
	}
	
	
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(s))
	
	
	log.Println("<-# NewPeerConnection")

	return api.NewPeerConnection(configuration)
}


func (this *Muxer) startPeerConnection() (error) {
	
	log.Println("#-> Muxer::startPeerConnection")
	
	newpc, err := this.NewPeerConnection(webrtc.Configuration{
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
	})
	this.pc = newpc
	
	if err != nil {
		log.Println("<-# Muxer::startPeerConnection FAILED! Cant create Offer")
		log.Println(err)
		return err
	}
	
	this.pc.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		
		this.status = connectionState
		if connectionState == webrtc.ICEConnectionStateDisconnected {
			log.Println("~~~~~~~~~~~~~~~ DISCONNECTED ~~~~~~~~~~~~~~~~~~")
			this.Close()
		} else if connectionState == webrtc.ICEConnectionStateConnected {
			log.Println("~~~~~~~~~~~~~~~ CONNECTED ~~~~~~~~~~~~~~~~~~")
		}
	})
	
	this.pc.OnDataChannel(func(d *webrtc.DataChannel) {
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			this.ClientACK.Reset(5 * time.Second)
		})
	})
	
	
	this.pc.OnNegotiationNeeded(func() {
		log.Println("#-> OnNegotiationNeeded")
		
		offer, err := this.pc.CreateOffer(nil)
		if err != nil {
			log.Println("<-# OnNegotiationNeeded FAILED! Cant create Offer")
			log.Println(err)
			this.Close()
			return
		}
		
		
		if (this.pc.SignalingState().String() != "stable") {
			log.Println("<-# OnNegotiationNeeded() Cant negotiate in this state: ",this.pc.SignalingState().String())
			return;
		}
		
		err = this.pc.SetLocalDescription(offer)
		if err != nil {
			log.Println("<-# OnNegotiationNeeded FAILED! Cant SetLocalDescription()")
			log.Println(err)
			this.Close()
			return
		}
		
		log.Println("	OnNegotiationNeeded() SetLocalDescription OK")
		response, err := json.Marshal(*this.pc.LocalDescription())
		if err != nil {
			log.Println("<-# OnNegotiationNeeded FAILED! Cant marshal local description")
			log.Println(err)
			this.Close()
			return
		}
		
		
		log.Println("	OnNegotiationNeeded() marshal OK")
		if err = this.ws.WriteMessage(1, response); err != nil {
			log.Println("<-# OnNegotiationNeeded FAILED! Cant send answer over websocket")
			log.Println(err)
			this.Close()
		}
		log.Println("<-# OnNegotiationNeeded")
	})

	
	this.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		log.Println("#-> OnICECandidate()")
		if candidate == nil {
			return
		}
		
		request, err := json.Marshal(&Candidate{Candidate: candidate})
		if err != nil {
			log.Println("<-# OnICECandidate FAILED! Cant marshal local description")
			log.Println(err)
			this.Close()
			return
		}
		
		log.Println("	OnICECandidate() marshal OK", string(request))
		if err = this.ws.WriteMessage(1, request); err != nil {
			log.Println("<-# OnICECandidate FAILED! Cant send candidate over websocket")
			log.Println(err)
			this.Close()
			return
		}
		
		log.Println("<-# OnICECandidate() OK")
	})
	
	log.Println("<-# Muxer::startPeerConnection OK")
	return nil
}

func (this *Muxer) handleAddTrack(streamId string) (error) {
	log.Println("#-> Muxer::addTrack ", streamId)
	if !Config.ext(streamId) {
		log.Println("Stream Not Found")
		return ErrorNotFound
	}
	Config.startRtspStreamIfNeeded(streamId)
	
	codecs := Config.codecDataGet(streamId)
	if (codecs == nil || (len(codecs) == 0)) {
		log.Println("Stream Codec Not Found")
		return ErrorNotFound
	}

	var AudioOnly bool
	if len(codecs) == 1 && codecs[0].Type().IsAudio() {
		AudioOnly = true
	}
	
	var success bool = false
	
	defer func() {
		if !success {
			err := this.Close()
			if err != nil {
				log.Println(err)
			}
		}
	}()


	var err error
	for i, i2 := range codecs {
		var track *webrtc.TrackLocalStaticSample = nil
		var rtpSender *webrtc.RTPSender = nil

		if i2.Type().IsVideo() {
		
			if i2.Type() == av.H264 {
				track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
					MimeType: "video/h264",
				}, "pion-rtsp-video", "pion-rtsp-video")
				if err != nil {
					return err
				}
				
				if rtpSender, err = this.pc.AddTrack(track); err != nil {
					return err
				}
				
				go func() {
						for {
								rtcpBuf := make([]byte, 1500)
								n, _, err := rtpSender.Read(rtcpBuf)
								if err != nil {
									log.Print(err)
									return
								}
								log.Printf("RTCP packet received: %s", hex.Dump(rtcpBuf[:n]))
						}
				}()
			}
		} else if i2.Type().IsAudio() {
			
			if i2.Type() == av.OPUS {
				track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
					MimeType:  webrtc.MimeTypeOpus,
					Channels:  uint16(i2.(av.AudioCodecData).ChannelLayout().Count()),
					ClockRate: uint32(i2.(av.AudioCodecData).SampleRate()),
				}, "pion-rtsp-audio", "pion-rtsp-audio")
				
				
				if err != nil {
					return err
				}
				if _, err = this.pc.AddTrack(track); err != nil {
					return err
				}
			}
		}
		this.rtpSenders[int8(i)] = rtpSender
		this.streams[int8(i)] = &Stream{track: track, codec: i2}
	}
	
	if (len(this.streams) == 0) {
		return ErrorNotTrackAvailable
	}
	go func() {
		cid, ch := Config.clientAdd(streamId)
		defer Config.clientDelete(streamId, cid)
		defer this.Close()
		var videoStart bool
		noVideo := time.NewTimer(10 * time.Second)
		for {
			select {
			case <-noVideo.C:
				log.Println("noVideo")
				return
			case pck := <-ch:
				if pck.IsKeyFrame || AudioOnly {
					noVideo.Reset(10 * time.Second)
					videoStart = true
				}
				if !videoStart && !AudioOnly {
					continue
				}
				err = this.WritePacket(pck)
				if err != nil {
					log.Println("WritePacket", err)
					return
				}
			}
		}
	}()
	success = true
	log.Println("<-# Muxer::addTrack")
	return nil

}

func (this *Muxer) WritePacket(pkt av.Packet) (err error) {
	
	//log.Println("#-> Muxer::WritePacket", pkt.Time, this.Stop, webrtc.ICEConnectionStateConnected, pkt.Idx, this.streams[pkt.Idx])
	
	var WritePacketSuccess bool
	defer func() {
		if !WritePacketSuccess {
			this.Close()
		}
	}()
	
	if this.Stop {
		return ErrorClientOffline
	}
	
	
	if this.status == webrtc.ICEConnectionStateChecking {
		WritePacketSuccess = true
		return nil
	}
	
	if this.status != webrtc.ICEConnectionStateConnected {
		return nil
	}
	
	
	if tmp, ok := this.streams[pkt.Idx]; ok {
		
		
		this.StreamACK.Reset(10 * time.Second)
		
		if len(pkt.Data) < 5 {
			return nil
		}
		
		
		switch tmp.codec.Type() {
		case av.H264:
			codec := tmp.codec.(h264parser.CodecData)
			if pkt.IsKeyFrame {
				pkt.Data = append([]byte{0, 0, 0, 1}, bytes.Join([][]byte{codec.SPS(), codec.PPS(), pkt.Data[4:]}, []byte{0, 0, 0, 1})...)
			} else {
				pkt.Data = pkt.Data[4:]
			}
		case av.PCM_ALAW:
		case av.OPUS:
		case av.PCM_MULAW:
		case av.AAC:
			//TODO: NEED ADD DECODER AND ENCODER
			return ErrorCodecNotSupported
		case av.PCM:
			//TODO: NEED ADD ENCODER
			return ErrorCodecNotSupported
		default:
			return ErrorCodecNotSupported
		}
		
		
		
		err = tmp.track.WriteSample(media.Sample{Data: pkt.Data, Duration: pkt.Duration})
		if err == nil {
			WritePacketSuccess = true
		}
		
		
		return err
	} else {
		WritePacketSuccess = true
		return nil
	}
}

func (this *Muxer) Close() error {
	this.Stop = true
	if this.pc != nil {
		err := this.pc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func wsApi(c *gin.Context) {
	ws, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		 log.Println("error get connection")
		log.Fatal(err)
	}
	defer ws.Close()
	handleConnection(ws)
	log.Println("<-# wsApi() ok")
}

var upGrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}


func serveHTTP() {
	
	log.Println("#-> main")
	
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()
	
	if _, err := os.Stat("./web"); !os.IsNotExist(err) {
		router.LoadHTMLGlob("web/templates/*")
		router.GET("/", HTTPAPIServerIndex)
		router.GET("/stream/player/:uuid", HTTPAPIServerStreamPlayer)
	}
	router.GET("/stream/codec/:uuid", HTTPAPIServerStreamCodec)
	router.GET("/ws", wsApi)

	router.StaticFS("/static", http.Dir("web/static"))
	err := router.Run(Config.Server.HTTPPort)
	if err != nil {
		log.Fatalln("Start HTTP Server error", err)
	}
}

func HTTPAPIServerIndex(c *gin.Context) {
	
	log.Println("#-> main")
	
	_, all := Config.list()
	if len(all) > 0 {
		c.Header("Cache-Control", "no-cache, max-age=0, must-revalidate, no-store")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Redirect(http.StatusMovedPermanently, "stream/player/"+all[0])
	} else {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"port":    Config.Server.HTTPPort,
			"version": time.Now().String(),
		})
	}
}

func HTTPAPIServerStreamPlayer(c *gin.Context) {
	
	log.Println("#-> HTTPAPIServerStreamPlayer")
	
	_, all := Config.list()
	sort.Strings(all)
	c.HTML(http.StatusOK, "player.tmpl", gin.H{
		"port":     Config.Server.HTTPPort,
		"suuid":    c.Param("uuid"),
		"suuidMap": all,
		"version":  time.Now().String(),
	})
	
	log.Println("<-# HTTPAPIServerStreamPlayer")
}

func HTTPAPIServerStreamCodec(c *gin.Context) {
	
	log.Println("#-> HTTPAPIServerStreamCodec")
	
	if Config.ext(c.Param("uuid")) {
		
		Config.startRtspStreamIfNeeded(c.Param("uuid"))
		codecs := Config.codecDataGet(c.Param("uuid"))
		
		if codecs == nil {
			return
		}
		
		var tmpCodec []JCodec
		
		for _, codec := range codecs {
			if codec.Type() != av.H264 && codec.Type() != av.OPUS {
				log.Println("Codec Not Supported WebRTC ignore this track", codec.Type())
				continue
			}
			if codec.Type().IsVideo() {
				tmpCodec = append(tmpCodec, JCodec{Type: "video"})
			} else {
				tmpCodec = append(tmpCodec, JCodec{Type: "audio"})
			}
		}
		b, err := json.Marshal(tmpCodec)
		if err == nil {
			_, err = c.Writer.Write(b)
			if err != nil {
				log.Println("Write Codec Info error", err)
				return
			}
		}
	}
	
	log.Println("<-# HTTPAPIServerStreamCodec")
}

