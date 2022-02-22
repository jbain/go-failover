package go_failover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"time"
)

type TransportHttp struct {
	Key        string
	ListenPort uint16
	router     *mux.Router
	groups     map[string]httpGroup
}

type httpGroup struct {
	inboundAnnounce  chan InboundAdvertisement
	inboundAdvertise chan Advertisement
}

func NewHttpTransport(key string, port uint16) *TransportHttp {
	return &TransportHttp{
		Key:        key,
		ListenPort: port,
		groups:     make(map[string]httpGroup),
	}
}

func (t *TransportHttp) Init() {
	t.router = mux.NewRouter()
	t.router.HandleFunc("/group/{groupId}", t.handleAnnouncement).Name("group")
	go http.ListenAndServe(fmt.Sprintf(":%d", t.ListenPort), t.router)

}

func (t *TransportHttp) handleAnnouncement(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	in := InboundAdvertisement{}
	vars := mux.Vars(r)
	err := json.NewDecoder(r.Body).Decode(&in)
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte(err.Error()))
		return
	}

	g, ok := t.groups[vars["groupId"]]
	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		return
	}

	in.response = make(chan Advertisement, 1)

	g.inboundAnnounce <- in
	timeout := time.NewTimer(1 * time.Second)
	select {
	case adv := <-in.response:
		out, err := json.Marshal(adv)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte(err.Error()))
			return
		}
		rw.WriteHeader(http.StatusOK)
		rw.Write(out)
		return
	case <-timeout.C:
		rw.WriteHeader(http.StatusRequestTimeout)
		return
	}
}

func (t TransportHttp) Ready(groupId string) <-chan InboundAdvertisement {
	if g, ok := t.groups[groupId]; ok {
		return g.inboundAnnounce
	}

	g := httpGroup{
		inboundAnnounce:  make(chan InboundAdvertisement, 1),
		inboundAdvertise: make(chan Advertisement, 1),
	}

	t.groups[groupId] = g
	return g.inboundAnnounce
}

func (t TransportHttp) Advertise(peer Peer, advertisement Advertisement) (Advertisement, error) {
	var advResponse Advertisement
	url, err := t.router.Get("group").URL("groupId", advertisement.GroupId)
	url.Host = peer.Id
	url.Scheme = "http"

	body, err := json.Marshal(advertisement)
	if err != nil {
		log.Print(err)
		return advResponse, err
	}
	postBody := bytes.NewBuffer(body)
	resp, err := http.Post(url.String(), "application/json", postBody)
	if err != nil {
		return advResponse, err
	}

	err = json.NewDecoder(resp.Body).Decode(&advResponse)
	if err != nil {
		log.Print(err)
		return advResponse, err
	}
	return advResponse, nil
}
