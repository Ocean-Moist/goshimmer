package dagsvisualizer

import "github.com/iotaledger/goshimmer/packages/jsonmodels"

const (
	// MsgTypeTangleVertex is the type of the Tangle DAG vertex.
	MsgTypeTangleVertex byte = iota
	// MsgTypeTangleBooked is the type of the Tangle DAG confirmed message.
	MsgTypeTangleBooked
	// MsgTypeTangleConfirmed is the type of the Tangle DAG confirmed message.
	MsgTypeTangleConfirmed
	// MsgTypeFutureMarkerUpdated is the type of the future marker updated message.
	MsgTypeFutureMarkerUpdated
	// MsgTypeMarkerAWUpdated is the type of the marker's AW updated message.
	MsgTypeMarkerAWUpdated
	// MsgTypeUTXOVertex is the type of the UTXO DAG vertex.
	MsgTypeUTXOVertex
	// MsgTypeUTXOConfirmed is the type of the UTXO DAG vertex confirmed message.
	MsgTypeUTXOConfirmed
	// MsgTypeBranchVertex is the type of the branch DAG vertex.
	MsgTypeBranchVertex
	// MsgTypeBranchParentsUpdate is the type of the branch DAG vertex parents updated message.
	MsgTypeBranchParentsUpdate
	// MsgTypeBranchAWUpdate is the type of the branch DAG vertex AW updated message.
	MsgTypeBranchAWUpdate
)

type wsMessage struct {
	Type byte        `json:"type"`
	Data interface{} `json:"data"`
}

type tangleVertex struct {
	ID              string   `json:"ID"`
	StrongParentIDs []string `json:"strongParentIDs"`
	WeakParentIDs   []string `json:"weakParentIDs"`
	BranchID        string   `json:"branchID"`
	IsMarker        bool     `json:"isMarker"`
	ConfirmedTime   int64    `json:"confirmedTime"`
}

type tangleBooked struct {
	ID       string `json:"ID"`
	IsMarker bool   `json:"isMarker"`
	BranchID string `json:"branchID"`
}

type tangleFinalized struct {
	ID            string `json:"ID"`
	ConfirmedTime int64  `json:"confirmedTime"`
}

type tangleFutureMarkerUpdated struct {
	ID             string `json:"ID"`
	FutureMarkerID string `json:"futureMarkerID"`
}

type tangleMarkerAWUpdated struct {
	ID             string  `jsong:"ID"`
	ApprovalWeight float64 `json:"approvalWeight"`
}

type utxoVertex struct {
	MsgID          string              `json:"msgID"`
	ID             string              `json:"ID"`
	Inputs         []*jsonmodels.Input `json:"inputs"`
	Outputs        []string            `json:"outputs"`
	ApprovalWeight float64             `json:"approvalweight"`
	ConfirmedTime  int64               `json:"confirmedTime"`
}

type utxoConfirmed struct {
	ID             string  `json:"ID"`
	ApprovalWeight float64 `json:"approvalWeight"`
	ConfirmedTime  int64   `json:"confirmedTime"`
}

type branchVertex struct {
	ID             string                                 `json:"ID"`
	Type           string                                 `json:"type"`
	Parents        []string                               `json:"parents"`
	ApprovalWeight float64                                `json:"approvalWeight"`
	ConfirmedTime  int64                                  `json:"confirmedTime"`
	Conflicts      *jsonmodels.GetBranchConflictsResponse `json:"conflicts"`
}

type branchParentUpdate struct {
	ID      string   `json:"ID"`
	Parents []string `json:"parents"`
}

type branchAWUpdate struct {
	ID             string   `json:"ID"`
	Conflicts      []string `json:"conflicts"`
	ApprovalWeight float64  `json:"approvalWeight"`
}