let stream = new MediaStream();
let suuid = $('#suuid').val();

let config = {
	iceServers: [{
		urls: ["stun:stun.l.google.com:19302"]
	}]
};

const pc = new RTCPeerConnection(config);

let ioSend = (content) => {
	log(" SEND: " + JSON.stringify(content));
	ws.send(JSON.stringify(content));
}
pc.onicecandidate = ({candidate}) => ioSend(candidate);
pc.onnegotiationneeded = async () => {
	log('#-> onnegotiationneeded ')
	const offer = await pc.createOffer();
	if (pc.signalingState != "stable") return;
	await pc.setLocalDescription(offer);
	ioSend(pc.localDescription);
	log('<-# onnegotiationneeded ')
}


let log = msg => {
	document.getElementById('div').innerHTML += new Date().toISOString() + ' ' + msg + '<br>'
}

var ws = new WebSocket("ws://black.ogsts.eu:8083/ws");
ws.onopen = function(evt) {
	log("#-> ws.onopen");
	getCodecInfo();
	log("<-# ws.onopen");
};

ws.onclose = function(evt) {
	log("#-> ws.onclose");
	log("<-# ws.onclose");
};


ws.onmessage = async (event) => {
	log('#-> ws.onmessage ');
	log(event.data);
	var msg = JSON.parse(event.data);

	if (msg.type && msg.type == 'offer') {
		log('    ws.onmessage() OFFER ');

		if (pc.signalingState != "stable") {
			log('    ws.onmessage() OFFER -> ROLLBACK ');
			await Promise.all([
				pc.setLocalDescription({ type: "rollback" }),
				pc.setRemoteDescription(msg)
			]);
		} else {
			log('    ws.onmessage() setRemoteDescription ');
			await pc.setRemoteDescription(msg);
			log('    ws.onmessage() setRemoteDescription ');
			const answer = await pc.createAnswer();
			log('    ws.onmessage() setLocalDescription ');
			await pc.setLocalDescription(answer);
			ioSend(answer);
		}
		log('<-# ws.onmessage()');
		return
	}

	if (msg.type && msg.type == 'answer') {
		log('    ws.onmessage() ANSWER ');
		await pc.setRemoteDescription(msg);
		log('    ws.onmessage() ANSWER OK');
		log('<-# ws.onmessage()');
		return
	}

	if (msg.Candidate) {
		log('    ws.onmessage() CANDIDATE ');
		await pc.addIceCandidate(msg.Candidate)
		log('<-# ws.onmessage()');
		return
	}
	log('    ws.onmessage() UNKNOWN !');
	log('<-# ws.onmessage()');
};

window.addTrack = async () => {
	log('#-> addTrack()')
	ioSend({type:"addTrack",Param: suuid })
	log('<-# addTrack() ' + JSON.stringify(result))
}

pc.ontrack = function(event) {
	log("#-> pc.ontrack");
	stream.addTrack(event.track);
	videoElem.srcObject = stream;
	log(event.streams.length + ' track is delivered')
	log("<-# pc.ontrack");
}

pc.oniceconnectionstatechange = e => {
	log(pc.iceConnectionState)
	if (pc.iceConnectionState =="connected" ) {
		ioSend({type:"addTrack",Param: suuid })
	}
}

$(document).ready(function() {
	$('#' + suuid).addClass('active');
});


function getCodecInfo() {

	log('#-> getCodecInfo()' + "codec/" + suuid)

	$.get("../codec/" + suuid, function(data) {
		try {
			log(data)
			data = JSON.parse(data);
		} catch (e) {
			console.log(e);
		} finally {

			$.each(data, function(index, value) {
				pc.addTransceiver(value.Type, { 'direction': 'sendrecv' })
			})
		}
	});

	log('<-# getCodecInfo')
}
