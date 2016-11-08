package types

// Requests

func ToRequestPrepare(proposerID string, proposalNumber uint64) *Request {
	return &Request{
		Value: &Request_Prepare{
			&RequestPrepare{proposerID, proposalNumber}},
	}
}

func ToRequestAccept(proposerID string, proposal *Proposal) *Request {
	return &Request{
		Value: &Request_Accept{
			&RequestAccept{proposerID, proposal}},
	}
}

func ToRequestChosen(proposerID string, proposal *Proposal) *Request {
	return &Request{
		Value: &Request_Chosen{
			&RequestChosen{proposerID, proposal}},
	}
}

// Responses

func ToResponsePrepare(acceptorID string, proposal *Proposal) *Response {
	return &Response{
		Value: &Response_Prepare{
			&ResponsePrepare{acceptorID, proposal}},
	}
}

func ToResponseReject(acceptorID string) *Response {
	return &Response{
		Value: &Response_Reject{
			&ResponseReject{acceptorID}},
	}
}

func ToResponseAccept(acceptorID string) *Response {
	return &Response{
		Value: &Response_Accept{
			&ResponseAccept{acceptorID}},
	}
}

func ToResponseChosen() *Response {
	return &Response{
		Value: &Response_Chosen{
			&ResponseChosen{}},
	}
}

// Reject
func IsResponseReject(res *Response) bool {
	return res.GetReject() != nil
}

// Updates
func ToUpdateAccept(acceptorID string, proposal *Proposal) *Update {
	return &Update{
		Value: &Update_Accept{
			&UpdateAccept{acceptorID, proposal}},
	}
}

func ToUpdateChosen(acceptorID string, proposal *Proposal) *Update {
	return &Update{
		Value: &Update_Chosen{
			&UpdateChosen{acceptorID, proposal}},
	}
}

//Decision
func ToDecision(learnerID string, proposal *Proposal) *Decision {
	return &Decision{learnerID, proposal}
}
