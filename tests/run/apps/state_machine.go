// A table-driven vending machine and a TCP-like connection state machine with event logs,
// invalid transition handling, and a replay verifier.
package main

import (
	"fmt"
	"sort"
	"strings"
)

type State int

const (
	Idle State = iota
	HasMoney
	Dispensing
	OutOfOrder
)

func (s State) String() string {
	return [...]string{"Idle", "HasMoney", "Dispensing", "OutOfOrder"}[s]
}

type Event interface{ isEvent() }

type Coin struct{ Cents int }
type Select struct{ Slot string }
type Refund struct{}
type Take struct{}
type Kick struct{}

func (Coin) isEvent()   {}
func (Select) isEvent() {}
func (Refund) isEvent() {}
func (Take) isEvent()   {}
func (Kick) isEvent()   {}

type Item struct {
	Name  string
	Price int
	Stock int
}

type Machine struct {
	state   State
	credit  int
	slots   map[string]*Item
	pending *Item
	bank    int
	log     []string
}

func NewMachine() *Machine {
	return &Machine{slots: map[string]*Item{
		"A1": {"cola", 150, 2},
		"A2": {"chips", 100, 1},
		"B1": {"gum", 50, 5},
	}}
}

func (m *Machine) logf(f string, a ...any) { m.log = append(m.log, fmt.Sprintf(f, a...)) }

func (m *Machine) Handle(ev Event) (err error) {
	from := m.state
	defer func() {
		if err != nil {
			m.logf("%v + %T: error: %v", from, ev, err)
		} else if from != m.state {
			m.logf("%v + %T -> %v", from, ev, m.state)
		}
	}()
	switch m.state {
	case OutOfOrder:
		if _, ok := ev.(Kick); ok {
			m.state = Idle
			return nil
		}
		return fmt.Errorf("machine is broken")
	case Idle, HasMoney:
		switch e := ev.(type) {
		case Coin:
			switch e.Cents {
			case 5, 10, 25, 50, 100:
				m.credit += e.Cents
				m.state = HasMoney
				return nil
			}
			return fmt.Errorf("rejected coin %d", e.Cents)
		case Select:
			it, ok := m.slots[e.Slot]
			switch {
			case !ok:
				return fmt.Errorf("no slot %q", e.Slot)
			case it.Stock == 0:
				return fmt.Errorf("%s sold out", it.Name)
			case m.credit < it.Price:
				return fmt.Errorf("need %d more for %s", it.Price-m.credit, it.Name)
			}
			m.credit -= it.Price
			m.bank += it.Price
			it.Stock--
			m.pending = it
			m.state = Dispensing
			return nil
		case Refund:
			if m.credit == 0 {
				return fmt.Errorf("nothing to refund")
			}
			m.logf("refunded %d", m.credit)
			m.credit = 0
			m.state = Idle
			return nil
		case Kick:
			m.state = OutOfOrder
			return nil
		}
	case Dispensing:
		switch ev.(type) {
		case Take:
			m.logf("took %s", m.pending.Name)
			m.pending = nil
			if m.credit > 0 {
				m.state = HasMoney
			} else {
				m.state = Idle
			}
			return nil
		case Kick:
			m.state = OutOfOrder
			return nil
		}
		return fmt.Errorf("take your item first")
	}
	return fmt.Errorf("unhandled event %T in %v", ev, m.state)
}

// TCP-like connection.
type Conn int

const (
	Closed Conn = iota
	Listen
	SynSent
	SynRcvd
	Established
	FinWait1
	FinWait2
	CloseWait
	LastAck
	TimeWait
)

var connNames = []string{"CLOSED", "LISTEN", "SYN_SENT", "SYN_RCVD", "ESTABLISHED", "FIN_WAIT_1", "FIN_WAIT_2", "CLOSE_WAIT", "LAST_ACK", "TIME_WAIT"}

func (c Conn) String() string { return connNames[c] }

type transition struct {
	from  Conn
	event string
}

var table = map[transition]Conn{
	{Closed, "passive_open"}: Listen,
	{Closed, "active_open"}:  SynSent,
	{Listen, "syn"}:          SynRcvd,
	{Listen, "send"}:         SynSent,
	{SynSent, "syn_ack"}:     Established,
	{SynSent, "syn"}:         SynRcvd,
	{SynRcvd, "ack"}:         Established,
	{Established, "close"}:   FinWait1,
	{Established, "fin"}:     CloseWait,
	{FinWait1, "ack"}:        FinWait2,
	{FinWait2, "fin"}:        TimeWait,
	{CloseWait, "close"}:     LastAck,
	{LastAck, "ack"}:         Closed,
	{TimeWait, "timeout"}:    Closed,
	{FinWait1, "fin"}:        TimeWait,
	{SynRcvd, "close"}:       FinWait1,
	{SynSent, "close"}:       Closed,
}

func run(events ...string) (Conn, []string, error) {
	c := Closed
	var trace []string
	for _, e := range events {
		next, ok := table[transition{c, e}]
		if !ok {
			return c, trace, fmt.Errorf("invalid event %q in state %v", e, c)
		}
		trace = append(trace, fmt.Sprintf("%v-%s->%v", c, e, next))
		c = next
	}
	return c, trace, nil
}

func main() {
	m := NewMachine()
	script := []Event{
		Coin{25}, Coin{7}, Select{"A1"}, Coin{100}, Select{"A1"}, Coin{25}, Take{}, Select{"B1"},
		Take{}, Refund{}, Coin{50}, Select{"B1"}, Take{}, Select{"A2"}, Coin{100}, Select{"A2"}, Take{},
		Coin{100}, Select{"A2"}, Select{"Z9"}, Refund{}, Kick{}, Coin{25}, Kick{}, Coin{50}, Refund{},
	}
	for _, ev := range script {
		m.Handle(ev)
	}
	fmt.Println(strings.Join(m.log, "\n"))
	fmt.Println("state:", m.state, "credit:", m.credit, "bank:", m.bank)
	var names []string
	for k := range m.slots {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("%s %s stock=%d\n", k, m.slots[k].Name, m.slots[k].Stock)
	}

	scenarios := [][]string{
		{"active_open", "syn_ack", "close", "ack", "fin", "timeout"},
		{"passive_open", "syn", "ack", "fin", "close", "ack"},
		{"active_open", "close"},
		{"passive_open", "ack"},
		{"active_open", "syn_ack", "close", "fin", "timeout"},
		{},
	}
	for _, s := range scenarios {
		end, trace, err := run(s...)
		fmt.Printf("%d events -> %v err=%v\n  %s\n", len(s), end, err, strings.Join(trace, " "))
	}
	reach := map[Conn]bool{Closed: true}
	for changed := true; changed; {
		changed = false
		for t, to := range table {
			if reach[t.from] && !reach[to] {
				reach[to] = true
				changed = true
			}
		}
	}
	n := 0
	for c := Closed; c <= TimeWait; c++ {
		if reach[c] {
			n++
		}
	}
	fmt.Println("reachable states:", n, "of", len(connNames), "transitions:", len(table))
}
