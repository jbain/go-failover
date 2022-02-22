package go_failover

import (
	"errors"
	"fmt"
	"log"
	"testing"
	"time"
)

type mockAnnounceResponse struct {
	Ad  Advertisement
	Err error
}

type mockTransport struct {
	AdvertiseCount     int
	AdvertiseResponses []mockAnnounceResponse
	InboundAnnounceCh  chan InboundAdvertisement
}

func (m *mockTransport) Ready(groupId string) <-chan InboundAdvertisement {
	inboundAnnounce := make(chan InboundAdvertisement, 1)
	inboundAdvertise := make(chan Advertisement, 1)

	return inboundAnnounce
}

func (m *mockTransport) Advertise(peer Peer, announcement Advertisement) (Advertisement, error) {
	m.AdvertiseCount++
	if len(m.AdvertiseResponses) > 0 {
		r := m.AdvertiseResponses[0]
		m.AdvertiseResponses = m.AdvertiseResponses[1:]
		return r.Ad, r.Err
	}
	return Advertisement{}, errors.New("no responses")

}

type mockCallback struct {
	prefix                    string
	ActiveCount, StandbyCount int
}

func (c *mockCallback) Active() {
	log.Printf("%s Active", c.prefix)
	c.ActiveCount++
}
func (c *mockCallback) Standby() {
	log.Printf("%s Standby", c.prefix)
	c.StandbyCount++
}

func TestGroup_Standalone(t *testing.T) {
	transport := &mockTransport{}
	callbacks := &mockCallback{}

	New("group1", "node1", DefaultPriority, 1000, transport,
		callbacks.Active,
		callbacks.Standby)

	time.Sleep(1 * time.Second)
	if callbacks.ActiveCount != 1 {
		t.Error("Should have transitioned active once")
	}

	if transport.AdvertiseCount != 0 {
		t.Error("Advertise count should be zero")
	}
}

func TestGroup_Standby(t *testing.T) {
	transport := &mockTransport{
		AdvertiseResponses: []mockAnnounceResponse{
			{
				Ad: Advertisement{
					Local: Peer{State: Active},
				},
			},
		},
	}
	callbacks := &mockCallback{}

	New("group1", "node1", DefaultPriority, 1000, transport,
		callbacks.Active,
		callbacks.Standby,
		"node2")

	time.Sleep(1 * time.Second)

	if transport.AdvertiseCount != 1 {
		t.Errorf("Announce count should be 1, %d", transport.AdvertiseCount)
	}
	if callbacks.StandbyCount != 1 && callbacks.ActiveCount != 0 {
		t.Errorf("Should be in standby after 1 transition:%d/%d", callbacks.StandbyCount, callbacks.ActiveCount)
	}
}

func TestGroup_Standby_Recurse(t *testing.T) {
	transport := &mockTransport{
		AdvertiseResponses: []mockAnnounceResponse{
			{
				Ad: Advertisement{
					Local:  Peer{State: Standby},
					Active: Peer{Id: "node3", State: Active},
				},
			},
			{
				Ad: Advertisement{
					Local: Peer{Id: "node3", State: Active},
				},
			},
		},
	}
	callbacks := &mockCallback{}

	New("group1", "node1", DefaultPriority, 1000, transport,
		callbacks.Active,
		callbacks.Standby,
		"node2")

	time.Sleep(1 * time.Second)

	if transport.AdvertiseCount != 2 {
		t.Errorf("Announce count should be 2, %d", transport.AdvertiseCount)
	}
	if callbacks.StandbyCount != 1 && callbacks.ActiveCount != 0 {
		t.Errorf("Should be in standby after 1 transition:%d/%d", callbacks.StandbyCount, callbacks.ActiveCount)
	}
}

func TestPeerList_Add(t *testing.T) {
	peers := PeerList{}

	peers.Add(Peer{
		Id:       "localhost2339",
		Priority: 100,
		State:    "",
	})

	peers.Add(Peer{
		Id:       "localhost2339",
		Priority: 110,
		State:    "",
	})

	peers.Add(Peer{
		Id:       "localhost2340",
		Priority: 120,
		State:    "",
	})

	fmt.Println(peers)
}
