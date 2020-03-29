package sealing

import (
	"github.com/filecoin-project/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
)

type mutator interface {
	apply(state *SectorInfo)
}

// globalMutator is an event which can apply in every state
type globalMutator interface {
	// applyGlobal applies the event to the state. If if returns true,
	//  event processing should be interrupted
	applyGlobal(state *SectorInfo) bool
}

// Global events

type SectorRestart struct{}

func (evt SectorRestart) applyGlobal(*SectorInfo) bool { return false }

type SectorFatalError struct{ error }

func (evt SectorFatalError) FormatError(xerrors.Printer) (next error) { return evt.error }

func (evt SectorFatalError) applyGlobal(state *SectorInfo) bool {
	log.Errorf("Fatal error on sector %d: %+v", state.SectorID, evt.error)
	// TODO: Do we want to mark the state as unrecoverable?
	//  I feel like this should be a softer error, where the user would
	//  be able to send a retry event of some kind
	return true
}

type SectorForceState struct {
	State api.SectorState
}

func (evt SectorForceState) applyGlobal(state *SectorInfo) bool {
	state.State = evt.State
	return true
}

// Normal path

type SectorStart struct {
	ID         abi.SectorNumber
	SectorType abi.RegisteredProof
	Pieces     []ffiwrapper.Piece
}

func (evt SectorStart) apply(state *SectorInfo) {
	state.SectorID = evt.ID
	state.Pieces = evt.Pieces
	state.SectorType = evt.SectorType
}

type SectorPacked struct{ Pieces []ffiwrapper.Piece }

func (evt SectorPacked) apply(state *SectorInfo) {
	state.Pieces = append(state.Pieces, evt.Pieces...)
}

type SectorPackingFailed struct{ error }

func (evt SectorPackingFailed) apply(*SectorInfo) {}

type SectorSealed struct {
	Sealed   cid.Cid
	Unsealed cid.Cid
	Ticket   api.SealTicket
}

func (evt SectorSealed) apply(state *SectorInfo) {
	commd := evt.Unsealed
	state.CommD = &commd
	commr := evt.Sealed
	state.CommR = &commr
	state.Ticket = evt.Ticket
}

type SectorSealFailed struct{ error }

func (evt SectorSealFailed) FormatError(xerrors.Printer) (next error) { return evt.error }
func (evt SectorSealFailed) apply(*SectorInfo)                        {}

type SectorPreCommitFailed struct{ error }

func (evt SectorPreCommitFailed) FormatError(xerrors.Printer) (next error) { return evt.error }
func (evt SectorPreCommitFailed) apply(*SectorInfo)                        {}

type SectorPreCommitted struct {
	Message cid.Cid
}

func (evt SectorPreCommitted) apply(state *SectorInfo) {
	state.PreCommitMessage = &evt.Message
}

type SectorSeedReady struct {
	Seed api.SealSeed
}

func (evt SectorSeedReady) apply(state *SectorInfo) {
	state.Seed = evt.Seed
}

type SectorComputeProofFailed struct{ error }

func (evt SectorComputeProofFailed) FormatError(xerrors.Printer) (next error) { return evt.error }
func (evt SectorComputeProofFailed) apply(*SectorInfo)                        {}

type SectorCommitFailed struct{ error }

func (evt SectorCommitFailed) FormatError(xerrors.Printer) (next error) { return evt.error }
func (evt SectorCommitFailed) apply(*SectorInfo)                        {}

type SectorCommitted struct {
	Message cid.Cid
	Proof   []byte
}

func (evt SectorCommitted) apply(state *SectorInfo) {
	state.Proof = evt.Proof
	state.CommitMessage = &evt.Message
}

type SectorProving struct{}

func (evt SectorProving) apply(*SectorInfo) {}

type SectorFinalized struct{}

func (evt SectorFinalized) apply(*SectorInfo) {}

type SectorFinalizeFailed struct{ error }

func (evt SectorFinalizeFailed) FormatError(xerrors.Printer) (next error) { return evt.error }
func (evt SectorFinalizeFailed) apply(*SectorInfo)                        {}

// Failed state recovery

type SectorRetrySeal struct{}

func (evt SectorRetrySeal) apply(state *SectorInfo) {}

type SectorRetryPreCommit struct{}

func (evt SectorRetryPreCommit) apply(state *SectorInfo) {}

type SectorRetryWaitSeed struct{}

func (evt SectorRetryWaitSeed) apply(state *SectorInfo) {}

// Faults

type SectorFaulty struct{}

func (evt SectorFaulty) apply(state *SectorInfo) {}

type SectorFaultReported struct{ reportMsg cid.Cid }

func (evt SectorFaultReported) apply(state *SectorInfo) {
	state.FaultReportMsg = &evt.reportMsg
}

type SectorFaultedFinal struct{}
