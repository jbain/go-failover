package go_failover

import (
	"errors"
	"log"
	"sort"
	"time"
)

const (
	Init    PeerState = "init"
	Active  PeerState = "active"
	Standby PeerState = "standby"
	Unknown PeerState = "unknown"

	DefaultPriority = 100
)

var (
	ErrPeerIsInit = errors.New("peer is in init state")
)

type PeerState string
type Priority uint8

type Transport interface {
	Ready(groupId string) (inboundAdvertise <-chan InboundAdvertisement)
	Advertise(peer Peer, advertisement Advertisement) (Advertisement, error)
}

type Peer struct {
	Id       string    `json:"id"`
	Priority Priority  `json:"priority"`
	State    PeerState `json:"state"`
}

type PeerList []Peer

func (l *PeerList) Add(peer Peer) {

	for idx, p := range *l {
		if p.Id == peer.Id {
			//If it already exists, update info
			(*l)[idx] = peer
			return
		}
	}
	*l = append(*l, peer)
	l.sort()
}

func (l *PeerList) sort() {
	sort.SliceStable(*l, func(i, j int) bool {
		return (*l)[i].Priority > (*l)[j].Priority
	})
}

type Advertisement struct {
	GroupId    string   `json:"group_id"`
	Local      Peer     `json:"local"`
	Active     Peer     `json:"active"`
	Standby    PeerList `json:"peers"`
	IntervalMs uint     `json:"interval_ms"`
}

type InboundAdvertisement struct {
	Advertisement
	response chan Advertisement
}

type Failover struct {
	Groups []Group
}

func New(groupId, localId string, localPriority uint8, intervalMs uint,
	transport Transport,
	becomeActiveCallback func(),
	becomeStandbyCallback func(),
	peerIds ...string) *Group {
	standbyPeers := PeerList{}
	for _, pId := range peerIds {
		standbyPeers.Add(Peer{
			Id:       pId,
			Priority: DefaultPriority,
			State:    Unknown,
		})
	}
	group := &Group{
		id: groupId,
		local: Peer{
			Id:       localId,
			Priority: Priority(localPriority),
			State:    Init,
		},
		active:        Peer{},
		standby:       standbyPeers,
		intervalMs:    intervalMs,
		becomeActive:  becomeActiveCallback,
		becomeStandby: becomeStandbyCallback,
		transport:     transport,
	}

	go group.loop()
	return group
}

type Group struct {
	id            string
	local         Peer
	active        Peer
	standby       PeerList
	intervalMs    uint
	becomeActive  func()
	becomeStandby func()
	transport     Transport
}

func (g *Group) loop() {
	var interval = time.Duration(g.intervalMs) * time.Millisecond
	intervalTicker := time.NewTicker(interval)
	activeDownCount := 0

	g.StateInit()
	inboundAds := g.transport.Ready(g.id)
	for {
		select {
		case ad := <-inboundAds:
			//we have received an announcement from an incoming peer
			log.Printf("Advertisement received: %#v", ad.Local)
			if g.local.State == Init {
				//We failed to fully initialize locally and a peer has announced to us
				if ad.Local.Priority > g.local.Priority {
					//they should be active
					g.active = ad.Local
					g.stateStandby()
				} else {
					//If our priority is higher or a tie
					//we should be active
					g.standby.Add(ad.Local)
					g.stateActive()
				}
			} else if g.local.State == Active {
				if ad.Local.State == Active {
					log.Printf("Contested Active: %+v", ad)
					//todo: newest wins
				}
				g.standby.Add(ad.Local)
			} else if g.local.State == Standby {
				if ad.Local.State != Active {
					//This is an add from a new peer
					//send them our current state and move on
					ad.response <- g.advertisement()
					break

				}
				if g.active.Id != ad.Active.Id && ad.Active.Id != "" {
					log.Printf("new active found: %s\n", ad.Active.Id)
					g.active = ad.Active
				}
				g.active = ad.Active
				g.standby = ad.Standby
				activeDownCount = 0
				intervalTicker.Reset(interval)
			}

			ad.response <- g.advertisement()

		case <-intervalTicker.C:
			if g.local.State == Active {

				// send advertisements to everyone!
				for _, peer := range g.standby {
					//might want to make this async
					log.Printf("Sending Advertisement:%s", peer.Id)
					_, err := g.transport.Advertise(peer, g.advertisement())
					if err != nil {
						log.Printf("Error Advertising: %s", err.Error())
					}
				}
			} else {
				activeDownCount++
				if activeDownCount > 3 {
					if g.local.State == Init {
						g.stateActive()
					} else {
						//start iterating through
						if len(g.standby) > 0 {
							next := g.standby[0]
							g.standby = g.standby[1:]
							if next.Id == g.local.Id {
								//we're next, assume active
								g.stateActive()
							}
						} else {
							//No known peers, assume active
							g.stateActive()
						}

					}
				}
			}
		}
	}
}

func (g *Group) StateInit() {
	log.Printf("[%s][%s] init begin", g.local.Id, g.id)
	defer log.Printf("[%s][%s] init complete", g.local.Id, g.id)
	g.local.State = Init

	if len(g.standby) == 0 {
		log.Printf("[%s][%s] no peers, assume active", g.local.Id, g.id)
		g.stateActive()
		return
	}

	claimActive := false
	for _, peer := range g.standby {
		adv, err := findActive(peer, g)
		if err != nil {
			log.Println(err)
			continue
		}
		if adv.Active.Id == g.local.Id {
			//peer suggests we should be active.
			//If no active peer is found, claim it
			claimActive = true
		}

		//found active
		g.active = adv.Local
		g.standby = adv.Standby
		g.stateStandby()
		return

	}

	if claimActive {
		g.stateActive()
	}
}

func findActive(peer Peer, g *Group) (Advertisement, error) {
	adv, err := g.transport.Advertise(peer, g.advertisement())
	if err != nil {
		return Advertisement{}, err
	}
	if adv.Local.State == Active {
		//found master
		return adv, nil
	} else if adv.Local.State == Standby {
		if adv.Active.Id == g.local.Id {
			//peer is saying we should be active
			return adv, nil
		}
		return findActive(adv.Active, g)
	} else {
		//peer is in Init
		return adv, ErrPeerIsInit
	}
}

func (g *Group) stateStandby() {
	g.local.State = Standby
	if g.becomeStandby != nil {
		g.becomeStandby()
	}
}

func (g *Group) stateActive() {
	g.local.State = Active
	g.active = g.local
	if g.becomeActive != nil {
		g.becomeActive()
	}
}

func (g *Group) advertisement() Advertisement {
	return Advertisement{
		GroupId:    g.id,
		Local:      g.local,
		Active:     g.active,
		Standby:    g.standby,
		IntervalMs: g.intervalMs,
	}
}
