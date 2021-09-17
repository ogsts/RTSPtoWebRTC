package main

import (
	"errors"
	"log"
	"time"

	"github.com/deepch/vdk/format/rtspv2"
)

var (
	ErrorStreamExitNoVideoOnStream = errors.New("Stream Exit No Video On Stream")
	ErrorStreamExitRtspDisconnect  = errors.New("Stream Exit Rtsp Disconnect")
	ErrorStreamExitNoViewer        = errors.New("Stream Exit On Demand No Viewer")
)

func serveStreams() {
	
	log.Println("#-> serveStreams() ")
	
	for k, v := range Config.Streams {
		if !v.OnDemand {
			go RTSPWorkerLoop(k, v.URL, v.OnDemand, v.DisableAudio, v.Debug)
		}
	}
	
	log.Println("<-# serveStreams() ")
}
func RTSPWorkerLoop(name, url string, OnDemand, DisableAudio, Debug bool) {
	log.Println("#-> RTSPWorkerLoop() ")
	defer Config.RunUnlock(name)
	for {
		log.Println("Stream Try Connect", name)
		err := RTSPWorker(name, url, OnDemand, DisableAudio, Debug)
		if err != nil {
			log.Println(err)
			Config.LastError = err
		}
		if OnDemand && !Config.HasViewer(name) {
			log.Println(ErrorStreamExitNoViewer)
			return
		}
		time.Sleep(1 * time.Second)
	}
	log.Println("<-# RTSPWorkerLoop() ")
}




func RTSPWorker(name, url string, OnDemand, DisableAudio, Debug bool) error {
	
	log.Println("#-> RTSPWorker() ", name, url, Debug)
	
	keyTest := time.NewTimer(20 * time.Second)
	clientTest := time.NewTimer(20 * time.Second)

	RTSPClient, err := rtspv2.Dial(rtspv2.RTSPClientOptions{URL: url, 
			DisableAudio: DisableAudio, 
			DialTimeout: 3 * time.Second,
			ReadWriteTimeout: 3 * time.Second,
			Debug: Debug})
	
	if err != nil {
		log.Println("<-# RTSPWorker() rtspv2.Dial failed ", name, url)
		return err
	}
	defer RTSPClient.Close()


	log.Println("    RTSPWorker() CodecData=", RTSPClient.CodecData)
	if RTSPClient.CodecData != nil {
		Config.codecDataSet(name, RTSPClient.CodecData)
	}
	
	
	var AudioOnly bool
	if len(RTSPClient.CodecData) == 1 && RTSPClient.CodecData[0].Type().IsAudio() {
		AudioOnly = true
	}
	
	for {
		select {

		case <-clientTest.C:

			if OnDemand {
				if !Config.HasViewer(name) {
					return ErrorStreamExitNoViewer
				} else {
					clientTest.Reset(20 * time.Second)
				}
			}
		case <-keyTest.C:
			return ErrorStreamExitNoVideoOnStream

		case signals := <-RTSPClient.Signals:
			switch signals {
			case rtspv2.SignalCodecUpdate:
				Config.codecDataSet(name, RTSPClient.CodecData)
			case rtspv2.SignalStreamRTPStop:
				return ErrorStreamExitRtspDisconnect
			}
			
			
		case packetAV := <-RTSPClient.OutgoingPacketQueue:
			if AudioOnly || packetAV.IsKeyFrame {
				keyTest.Reset(20 * time.Second)
			}
			Config.cast(name, *packetAV)
		}
	}
}
