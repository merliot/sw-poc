package hub

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/websocket"
)

func newConfig(url *url.URL, user, passwd string) (*websocket.Config, error) {
	var surl = url.String()
	var origin = "http://localhost/"

	// Configure the websocket
	config, err := websocket.NewConfig(surl, origin)
	if err != nil {
		return nil, err
	}

	// If valid user, set the basic auth header for the request
	if user != "" {
		req, err := http.NewRequest("GET", surl, nil)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(user, passwd)
		config.Header = req.Header
	}

	return config, nil
}

func wsDial(url *url.URL, user, passwd string) {
	cfg, err := newConfig(url, user, passwd)
	if err != nil {
		slog.Error("Configuring websocket", "err", err)
		return
	}

	for {
		// Dial the websocket
		conn, err := websocket.DialConfig(cfg)
		if err == nil {
			// Service the client websocket
			wsClient(conn)
		} else {
			slog.Error("Dialing", "url", url, "err", err)
		}

		// Try again in a second
		time.Sleep(time.Second)
	}
}

func wsClient(conn *websocket.Conn) {
	defer conn.Close()

	var link = &wsLink{conn: conn}
	var ann = announcement{
		Id:           root.Id,
		Model:        root.Model,
		Name:         root.Name,
		DeployParams: root.DeployParams,
	}
	var pkt = &Packet{
		Dst:  ann.Id,
		Path: "/announce",
	}

	pkt.Marshal(&ann)

	// Send announcement
	slog.Info("Sending announcement", "pkt", pkt)
	err := link.Send(pkt)
	if err != nil {
		slog.Error("Sending", "err", err)
		return
	}

	// Receive welcome within 1 sec
	pkt, err = link.receiveTimeout(time.Second)
	if err != nil {
		slog.Error("Receiving", "err", err)
		return
	}

	slog.Info("Reply from announcement", "pkt", pkt)
	if pkt.Path != "/welcome" {
		slog.Error("Not welcomed, got", "path", pkt.Path)
		return
	}

	//slog.Info("Adding Uplink")
	uplinksAdd(link)

	// Send /state packets to all devices
	devicesSendState(link)

	// Route incoming packets down to the destination device.  Stop and
	// disconnect on EOF.

	slog.Info("Receiving packets")
	for {
		pkt, err := link.receivePoll()
		if err != nil {
			slog.Error("Receiving packet", "err", err)
			break
		}
		slog.Info("Route packet DOWN", "pkt", pkt)
		deviceRouteDown(pkt.Dst, pkt)
	}

	slog.Info("Removing Uplink")
	uplinksRemove(link)
}
