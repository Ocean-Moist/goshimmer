package dagsvisualizer

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/iotaledger/goshimmer/packages/consensus/gof"
	"github.com/iotaledger/goshimmer/packages/jsonmodels"
	"github.com/iotaledger/goshimmer/packages/ledgerstate"
	"github.com/iotaledger/goshimmer/packages/shutdown"
	"github.com/iotaledger/goshimmer/packages/tangle"
	"github.com/iotaledger/hive.go/daemon"
	"github.com/iotaledger/hive.go/datastructure/walker"
	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/workerpool"
	"github.com/labstack/echo"
)

var (
	visualizerWorkerCount     = 1
	visualizerWorkerQueueSize = 500
	visualizerWorkerPool      *workerpool.NonBlockingQueuedWorkerPool
)

func setupVisualizer() {
	// create and start workerpool
	visualizerWorkerPool = workerpool.NewNonBlockingQueuedWorkerPool(func(task workerpool.Task) {
		broadcastWsMessage(task.Param(0))
	}, workerpool.WorkerCount(visualizerWorkerCount), workerpool.QueueSize(visualizerWorkerQueueSize))
}

func runVisualizer() {
	if err := daemon.BackgroundWorker("Dags Visualizer[Visualizer]", func(ctx context.Context) {
		// register to events
		registerTangleEvents()
		registerUTXOEvents()
		registerBranchEvents()

		<-ctx.Done()
		log.Info("Stopping DAGs Visualizer ...")
		visualizerWorkerPool.Stop()
		log.Info("Stopping DAGs Visualizer ... done")
	}, shutdown.PriorityDashboard); err != nil {
		log.Panicf("Failed to start as daemon: %s", err)
	}
}

type searchResult struct {
	Messages []*tangleVertex `json:"messages"`
	Txs      []*utxoVertex   `json:"txs"`
	Branches []*branchVertex `json:"branches"`
}

func setupDagsVisualizerRoutes(routeGroup *echo.Group) {
	fmt.Println("set up visualizer route")
	routeGroup.GET("/dagsvisualizer/search/:start/:end", func(c echo.Context) (err error) {
		start, err := strconv.ParseInt(c.Param("start"), 10, 64)
		if err != nil {
			return
		}
		startTimestamp := time.Unix(start, 0)

		end, err := strconv.ParseInt(c.Param("end"), 10, 64)
		if err != nil {
			return
		}
		endTimestamp := time.Unix(end, 0)
		fmt.Println(startTimestamp.UnixNano(), endTimestamp.UnixNano())

		messages := []*tangleVertex{}
		txs := []*utxoVertex{}
		branches := []*branchVertex{}
		branchMap := make(map[ledgerstate.BranchID]struct{})

		// TODO: clean up below
		deps.Tangle.Utils.WalkMessageID(func(messageID tangle.MessageID, walker *walker.Walker) {
			deps.Tangle.Storage.Message(messageID).Consume(func(msg *tangle.Message) {
				// check the issuance time of a message is in the given time interval
				if msg.IssuingTime().After(startTimestamp) && msg.IssuingTime().Before(endTimestamp) {
					deps.Tangle.Storage.MessageMetadata(messageID).Consume(func(msgMetadata *tangle.MessageMetadata) {
						messages = append(messages, &tangleVertex{
							ID:              messageID.Base58(),
							StrongParentIDs: msg.ParentsByType(tangle.StrongParentType).ToStrings(),
							WeakParentIDs:   msg.ParentsByType(tangle.WeakParentType).ToStrings(),
							LikedParentIDs:  msg.ParentsByType(tangle.LikeParentType).ToStrings(),
							BranchID:        ledgerstate.UndefinedBranchID.Base58(),
							IsMarker:        msgMetadata.StructureDetails().IsPastMarker,
							IsTx:            msg.Payload().Type() == ledgerstate.TransactionType,
							ConfirmedTime:   msgMetadata.GradeOfFinalityTime().UnixNano(),
							GoF:             msgMetadata.GradeOfFinality().String(),
						})
					})

					// add tx
					if msg.Payload().Type() == ledgerstate.TransactionType {
						tx := msg.Payload().(*ledgerstate.Transaction)
						// handle inputs (retrieve outputID)
						inputs := make([]*jsonmodels.Input, len(tx.Essence().Inputs()))
						for i, input := range tx.Essence().Inputs() {
							inputs[i] = jsonmodels.NewInput(input)
						}

						outputs := make([]string, len(tx.Essence().Outputs()))
						for i, output := range tx.Essence().Outputs() {
							outputs[i] = output.ID().Base58()
						}

						txs = append(txs, &utxoVertex{
							MsgID:   messageID.Base58(),
							ID:      tx.ID().Base58(),
							Inputs:  inputs,
							Outputs: outputs,
							GoF:     gof.GradeOfFinality(0).String(),
						})
					}

					// add branch
					branchID, err := deps.Tangle.Booker.MessageBranchID(messageID)
					if err != nil {
						branchID = ledgerstate.BranchID{}
					}
					if _, ok := branchMap[branchID]; !ok {
						branchMap[branchID] = struct{}{}

						deps.Tangle.LedgerState.BranchDAG.Branch(branchID).Consume(func(branch ledgerstate.Branch) {
							conflicts := make(map[ledgerstate.ConflictID][]ledgerstate.BranchID)
							// get conflicts for Conflict branch
							if branch.Type() == ledgerstate.ConflictBranchType {
								for conflictID := range branch.(*ledgerstate.ConflictBranch).Conflicts() {
									conflicts[conflictID] = make([]ledgerstate.BranchID, 0)
									deps.Tangle.LedgerState.BranchDAG.ConflictMembers(conflictID).Consume(func(conflictMember *ledgerstate.ConflictMember) {
										conflicts[conflictID] = append(conflicts[conflictID], conflictMember.BranchID())
									})
								}
							}

							branches = append(branches, &branchVertex{
								ID:        branch.ID().Base58(),
								Type:      branch.Type().String(),
								Parents:   branch.Parents().Strings(),
								Conflicts: jsonmodels.NewGetBranchConflictsResponse(branch.ID(), conflicts),
								Confirmed: false,
							})
						})
					}
				}
			})

			deps.Tangle.Storage.Approvers(messageID).Consume(func(approver *tangle.Approver) {
				walker.Push(approver.ApproverMessageID())
			})
		}, tangle.MessageIDs{tangle.EmptyMessageID})

		return c.JSON(http.StatusOK, searchResult{Messages: messages, Txs: txs, Branches: branches})
	})
}

func registerTangleEvents() {
	storeClosure := events.NewClosure(func(messageID tangle.MessageID) {
		deps.Tangle.Storage.Message(messageID).Consume(func(msg *tangle.Message) {
			visualizerWorkerPool.TrySubmit(&wsMessage{
				Type: MsgTypeTangleVertex,
				Data: &tangleVertex{
					ID:              messageID.Base58(),
					StrongParentIDs: msg.ParentsByType(tangle.StrongParentType).ToStrings(),
					WeakParentIDs:   msg.ParentsByType(tangle.WeakParentType).ToStrings(),
					LikedParentIDs:  msg.ParentsByType(tangle.LikeParentType).ToStrings(),
					BranchID:        ledgerstate.UndefinedBranchID.Base58(),
					IsMarker:        false,
					IsTx:            msg.Payload().Type() == ledgerstate.TransactionType,
					ConfirmedTime:   0,
				},
			})
		})
	})

	bookedClosure := events.NewClosure(func(messageID tangle.MessageID) {
		deps.Tangle.Storage.MessageMetadata(messageID).Consume(func(msgMetadata *tangle.MessageMetadata) {
			visualizerWorkerPool.TrySubmit((&wsMessage{
				Type: MsgTypeTangleBooked,
				Data: &tangleBooked{
					ID:       messageID.Base58(),
					IsMarker: msgMetadata.StructureDetails().IsPastMarker,
					BranchID: msgMetadata.BranchID().Base58(),
				},
			}))
		})
	})

	msgConfirmedClosure := events.NewClosure(func(messageID tangle.MessageID) {
		deps.Tangle.Storage.MessageMetadata(messageID).Consume(func(msgMetadata *tangle.MessageMetadata) {
			visualizerWorkerPool.TrySubmit(&wsMessage{
				Type: MsgTypeTangleConfirmed,
				Data: &tangleConfirmed{
					ID:            messageID.Base58(),
					GoF:           msgMetadata.GradeOfFinality().String(),
					ConfirmedTime: msgMetadata.GradeOfFinalityTime().UnixNano(),
				},
			})
		})
	})

	fmUpdateClosure := events.NewClosure(func(fmUpdate *tangle.FutureMarkerUpdate) {
		visualizerWorkerPool.TrySubmit(&wsMessage{
			Type: MsgTypeFutureMarkerUpdated,
			Data: &tangleFutureMarkerUpdated{
				ID:             fmUpdate.ID.Base58(),
				FutureMarkerID: fmUpdate.FutureMarker.Base58(),
			},
		})
	})

	deps.Tangle.Storage.Events.MessageStored.Attach(storeClosure)
	deps.Tangle.Booker.Events.MessageBooked.Attach(bookedClosure)
	deps.Tangle.Booker.MarkersManager.Events.FutureMarkerUpdated.Attach(fmUpdateClosure)
	deps.FinalityGadget.Events().MessageConfirmed.Attach(msgConfirmedClosure)
}

func registerUTXOEvents() {
	storeClosure := events.NewClosure(func(messageID tangle.MessageID) {
		deps.Tangle.Storage.Message(messageID).Consume(func(msg *tangle.Message) {
			if msg.Payload().Type() == ledgerstate.TransactionType {
				tx := msg.Payload().(*ledgerstate.Transaction)
				// handle inputs (retrieve outputID)
				inputs := make([]*jsonmodels.Input, len(tx.Essence().Inputs()))
				for i, input := range tx.Essence().Inputs() {
					inputs[i] = jsonmodels.NewInput(input)
				}

				outputs := make([]string, len(tx.Essence().Outputs()))
				for i, output := range tx.Essence().Outputs() {
					outputs[i] = output.ID().Base58()
				}

				visualizerWorkerPool.TrySubmit(&wsMessage{
					Type: MsgTypeUTXOVertex,
					Data: &utxoVertex{
						MsgID:   messageID.Base58(),
						ID:      tx.ID().Base58(),
						Inputs:  inputs,
						Outputs: outputs,
						GoF:     gof.GradeOfFinality(0).String(),
					},
				})
			}
		})
	})

	txConfirmedClosure := events.NewClosure(func(txID ledgerstate.TransactionID) {
		deps.Tangle.LedgerState.TransactionMetadata(txID).Consume(func(txMetadata *ledgerstate.TransactionMetadata) {
			visualizerWorkerPool.TrySubmit(&wsMessage{
				Type: MsgTypeUTXOConfirmed,
				Data: &utxoConfirmed{
					ID:            txID.Base58(),
					GoF:           txMetadata.GradeOfFinality().String(),
					ConfirmedTime: txMetadata.GradeOfFinalityTime().UnixNano(),
				},
			})
		})
	})

	deps.Tangle.Storage.Events.MessageStored.Attach(storeClosure)
	deps.FinalityGadget.Events().TransactionConfirmed.Attach(txConfirmedClosure)
}

func registerBranchEvents() {
	createdClosure := events.NewClosure(func(branchID ledgerstate.BranchID) {
		deps.Tangle.LedgerState.BranchDAG.Branch(branchID).Consume(func(branch ledgerstate.Branch) {
			conflicts := make(map[ledgerstate.ConflictID][]ledgerstate.BranchID)
			// get conflicts for Conflict branch
			if branch.Type() == ledgerstate.ConflictBranchType {
				for conflictID := range branch.(*ledgerstate.ConflictBranch).Conflicts() {
					conflicts[conflictID] = make([]ledgerstate.BranchID, 0)
					deps.Tangle.LedgerState.BranchDAG.ConflictMembers(conflictID).Consume(func(conflictMember *ledgerstate.ConflictMember) {
						conflicts[conflictID] = append(conflicts[conflictID], conflictMember.BranchID())
					})
				}
			}

			visualizerWorkerPool.TrySubmit(&wsMessage{
				Type: MsgTypeBranchVertex,
				Data: &branchVertex{
					ID:        branch.ID().Base58(),
					Type:      branch.Type().String(),
					Parents:   branch.Parents().Strings(),
					Conflicts: jsonmodels.NewGetBranchConflictsResponse(branch.ID(), conflicts),
					Confirmed: false,
				},
			})
		})
	})
	// BranchParentUpdate
	parentUpdateClosure := events.NewClosure(func(parentUpdate *ledgerstate.BranchParentUpdate) {
		visualizerWorkerPool.TrySubmit(&wsMessage{
			Type: MsgTypeBranchParentsUpdate,
			Data: &branchParentUpdate{
				ID:      parentUpdate.ID.Base58(),
				Parents: parentUpdate.NewParents.Strings(),
			},
		})
	})

	branchConfirmedClosure := events.NewClosure(func(branchID ledgerstate.BranchID) {
		visualizerWorkerPool.TrySubmit(&wsMessage{
			Type: MsgTypeBranchConfirmed,
			Data: &branchConfirmed{
				ID: branchID.Base58(),
			},
		})
	})

	deps.Tangle.LedgerState.BranchDAG.Events.BranchCreated.Attach(createdClosure)
	deps.FinalityGadget.Events().BranchConfirmed.Attach(branchConfirmedClosure)
	deps.Tangle.LedgerState.BranchDAG.Events.BranchParentsUpdated.Attach(parentUpdateClosure)
}
