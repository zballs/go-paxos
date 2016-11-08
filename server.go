package gopaxos

import (
	pb "github.com/zballs/gopaxos/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"log"
	"sync"
)

// TODO: add mutexes

type Server struct {
	addr  string
	addrs []string
	index int

	proposalNumber  uint64 //initialized to 0
	highestResponse uint64

	highestAccept *pb.Proposal
	accepted      map[uint64]string //necessary?

	updates map[uint64]map[string]struct{}

	chosen    map[uint64]string
	chosenMtx sync.Mutex
}

func NewServer(addr string, addrs []string) *Server {

	return &Server{
		addr:     addr,
		addrs:    addrs,
		accepted: make(map[uint64]string),
		updates:  make(map[uint64]map[string]struct{}),
		chosen:   make(map[uint64]string),
	}
}

// Proposer

func (s *Server) numMajority() int {
	return len(s.addrs)/2 + 1
}

func (s *Server) addrsMajority() []string {
	numMajority := s.numMajority()
	if s.index+numMajority <= len(s.addrs) {
		return s.addrs[s.index : s.index+numMajority]
	}
	return append(s.addrs[s.index:], s.addrs[:len(s.addrs)-numMajority]...)
}

func (s *Server) incIndex() {
	s.index++
	if s.index >= len(s.addrs) {
		s.index = 0
	}
}

func (s *Server) MakeRequest(addr string, req *pb.Request) (*pb.Response, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	cli := pb.NewAgentClient(conn)
	res, err := cli.HandleRequest(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) ProposerRoutine() {

FOR_LOOP:
	for {
		// Phase 1A

		if s.proposalNumber < s.highestResponse {
			s.proposalNumber = s.highestResponse + 1
		}

		req := pb.ToRequestPrepare(s.addr, s.proposalNumber)

		var responses []*pb.Response
		highest := s.highestAccept

		// Send to majority of acceptors
		addrsMajority := s.addrsMajority()
		s.incIndex()

		for _, addr := range addrsMajority {
			res, err := s.MakeRequest(addr, req)
			if err != nil {
				log.Println(err.Error()) //for now
				continue
			}
			if pb.IsResponseReject(res) {
				// abandon proposal
				continue FOR_LOOP
			}
			if res == nil {
				// null res, shouldn't happen
				continue
			}
			// should we check to see if
			// response is from acceptor
			// we sent request to?
			prep := res.GetPrepare()
			if prep == nil {
				log.Printf("Proposer %s received unexpected response; expected prepare\n", s.addr)
				continue
			}
			proposal := prep.GetProposal()
			if highest.ProposalNumber < proposal.ProposalNumber {
				highest = proposal
			}
			responses = append(responses, res)
		}

		if len(responses) < s.numMajority() {
			// Did not get responses from
			// majority of acceptors
			continue
		}

		// Phase 2A

		req = pb.ToRequestAccept(s.addr, highest)
		responses = []*pb.Response{}

		for _, addr := range addrsMajority {
			res, err := s.MakeRequest(addr, req)
			if err != nil {
				log.Println(err.Error())
				continue
			}
			if pb.IsResponseReject(res) {
				// abandon proposal
				continue FOR_LOOP
			}
			if res == nil {
				// null res, shouldn't happen
				continue
			}
			accept := res.GetAccept()
			if accept == nil {
				log.Printf("Proposer %s received unexpected response; expected accept\n", s.addr)
				continue
			}
			responses = append(responses, res)
		}

		if len(responses) < s.numMajority() {
			// Did not get responses from
			// majority of acceptors
			continue
		}

		// Proposal is chosen

		s.chosenMtx.Lock()
		s.chosen[highest.ProposalNumber] = highest.Value
		s.chosenMtx.Unlock()

		req = pb.ToRequestChosen(s.addr, highest)

		for _, addr := range addrsMajority {
			_, err := s.MakeRequest(addr, req)
			if err != nil {
				log.Println(err.Error())
			}
			// ignore responses
		}
	}
}

// Acceptor

func (s *Server) HandleDecision(ctx context.Context, decision *pb.Decision) *pb.Empty {
	proposal := decision.Proposal
	proposalNumber := proposal.ProposalNumber
	value := proposal.Value
	s.chosenMtx.Lock()
	defer s.chosenMtx.Unlock()
	s.chosen[proposalNumber] = value
	return nil
}

func (s *Server) HandleRequest(ctx context.Context, req *pb.Request) *pb.Response {
	switch req.Value.(type) {
	case *pb.Request_Prepare:
		log.Printf("Acceptor %s received prepare request\n", s.addr)
		proposalNumber := req.GetPrepare().ProposalNumber
		if proposalNumber <= s.highestResponse {
			res := pb.ToResponseReject(s.addr)
			return res
		}
		s.highestResponse = proposalNumber
		res := pb.ToResponsePrepare(s.addr, s.highestAccept)
		return res
	case *pb.Request_Accept:
		log.Printf("Acceptor %s received accept request\n", s.addr)
		reqAccept := req.GetAccept()
		proposal := reqAccept.GetProposal()
		if proposal.ProposalNumber < s.highestResponse {
			res := pb.ToResponseReject(s.addr)
			return res
		}
		s.highestAccept = proposal
		s.accepted[proposal.ProposalNumber] = proposal.Value
		proposer := reqAccept.ProposerID
		update := pb.ToUpdateAccept(s.addr, s.highestAccept)
		go s.BroadcastUpdates(proposer, update)
		res := pb.ToResponseAccept(s.addr)
		return res
	case *pb.Request_Chosen:
		log.Printf("Acceptor %s received chosen request\n", s.addr)
		res := pb.ToResponseChosen()
		reqChosen := req.GetChosen()
		proposal := reqChosen.GetProposal()
		proposalNumber := proposal.ProposalNumber
		if _, exists := s.chosen[proposalNumber]; exists {
			// Learner already informed acceptor
			// that proposal was chosen...
			return res
		}
		s.chosenMtx.Lock()
		s.chosen[proposalNumber] = proposal.Value
		s.chosenMtx.Unlock()
		proposer := reqChosen.ProposerID
		update := pb.ToUpdateChosen(s.addr, proposal)
		go s.BroadcastUpdates(proposer, update)
		return res
	default:
		return nil
	}

}

func (s *Server) BroadcastUpdates(proposer string, update *pb.Update) {
	sentTo := 0
	numMajority := s.numMajority()
	for {
		for _, addr := range s.addrsMajority() {
			if addr == proposer {
				continue
			}
			err := s.SendUpdate(addr, update)
			if err != nil {
				log.Println(err.Error())
				continue
			}
			sentTo++
			if sentTo == numMajority {
				return
			}
		}
	}
}

func (s *Server) SendUpdate(addr string, update *pb.Update) error {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	cli := pb.NewAgentClient(conn)
	_, err = cli.HandleUpdate(context.Background(), update)
	return err
}

// Learner

func (s *Server) HandleUpdate(ctx context.Context, update *pb.Update) *pb.Empty {
	switch update.Value.(type) {
	case *pb.Update_Accept:
		updateAccept := update.GetAccept()
		proposal := updateAccept.GetProposal()
		proposalNumber := proposal.ProposalNumber
		value := proposal.Value
		acceptor := updateAccept.AcceptorID
		if _, ok := s.updates[proposalNumber]; !ok {
			s.updates[proposalNumber] = make(map[string]struct{})
		}
		s.updates[proposalNumber][acceptor] = struct{}{}
		if len(s.updates[proposalNumber]) < s.numMajority() {
			return nil
		}
		// We have majority, broadcast decisions
		delete(s.updates, proposalNumber)
		decision := pb.ToDecision(s.addr, proposal)
		go s.BroadcastDecisions("", decision)
		s.chosenMtx.Lock()
		defer s.chosenMtx.Unlock()
		s.chosen[proposalNumber] = value
		return nil
	case *pb.Update_Chosen:
		updateChosen := update.GetChosen()
		proposal := updateChosen.GetProposal()
		proposalNumber := proposal.ProposalNumber
		value := proposal.Value
		acceptor := updateChosen.AcceptorID
		delete(s.updates, proposalNumber)
		decision := pb.ToDecision(s.addr, proposal)
		go s.BroadcastDecisions(acceptor, decision)
		s.chosenMtx.Lock()
		defer s.chosenMtx.Unlock()
		s.chosen[proposalNumber] = value
		return nil
	default:
		return nil
	}
}

func (s *Server) BroadcastDecisions(acceptor string, decision *pb.Decision) {
	sentTo := 0
	numMajority := s.numMajority()
	for {
		for _, addr := range s.addrsMajority() {
			if addr == acceptor {
				continue
			}
			err := s.SendDecision(addr, decision)
			if err != nil {
				log.Println(err.Error())
				continue
			}
			sentTo++
			if sentTo == numMajority {
				return
			}
		}
	}
}

func (s *Server) SendDecision(addr string, decision *pb.Decision) error {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	cli := pb.NewAgentClient(conn)
	_, err = cli.HandleDecision(context.Background(), decision)
	return err
}
