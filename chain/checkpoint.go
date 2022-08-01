package chain

import (
	"context"
	"encoding/json"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipfs/go-datastore"
	"golang.org/x/xerrors"
)

func (syncer *Syncer) SyncCheckpoint(ctx context.Context, tsk types.TipSetKey) error {
	if tsk == types.EmptyTSK {
		return xerrors.Errorf("called with empty tsk")
	}
	ts, err := syncer.ChainStore().LoadTipSet(ctx, tsk)
	if err != nil {
		tss, err := syncer.Exchange.GetBlocks(ctx, tsk, 1)
		if err != nil {
			return xerrors.Errorf("failed to fetch tipset: %w", err)
		} else if len(tss) != 1 {
			return xerrors.Errorf("expected 1 tipset, got %d", len(tss))
		}
		ts = tss[0]
	}

	if err := syncer.switchChain(ctx, ts); err != nil {
		return xerrors.Errorf("failed to switch chain when syncing checkpoint: %w", err)
	}

	if err := syncer.ChainStore().SetCheckpoint(ctx, ts); err != nil {
		return xerrors.Errorf("failed to set the chain checkpoint: %w", err)
	}

	return nil
}

func (syncer *Syncer) switchChain(ctx context.Context, ts *types.TipSet) error {
	hts := syncer.ChainStore().GetHeaviestTipSet()
	if hts.Equals(ts) {
		return nil
	}

	if anc, err := syncer.store.IsAncestorOf(ctx, ts, hts); err == nil && anc {
		return nil
	}

	// Otherwise, sync the chain and set the head.
	if err := syncer.collectChain(ctx, ts, hts, true); err != nil {
		return xerrors.Errorf("failed to collect chain for checkpoint: %w", err)
	}

	if err := syncer.ChainStore().SetHead(ctx, ts); err != nil {
		return xerrors.Errorf("failed to set the chain head: %w", err)
	}
	return nil
}

var CheckpointKey = datastore.NewKey("/chain/checks")

func loadCheckpoint(ds dtypes.MetadataDS) (types.TipSetKey, error) {
	ctx := context.TODO()
	haveChks, err := ds.Has(ctx, CheckpointKey)
	if err != nil {
		return types.EmptyTSK, err
	}

	if !haveChks {
		return types.EmptyTSK, nil
	}

	tskBytes, err := ds.Get(ctx, CheckpointKey)
	if err != nil {
		return types.EmptyTSK, err
	}

	var tsk types.TipSetKey
	err = json.Unmarshal(tskBytes, &tsk)
	if err != nil {
		return types.EmptyTSK, err
	}

	return tsk, err
}

func (syncer *Syncer) SetCheckpoint(tsk types.TipSetKey) error {
	if tsk == types.EmptyTSK {
		return xerrors.Errorf("called with empty tsk")
	}
	ctx := context.TODO()
	syncer.checkptLk.Lock()
	defer syncer.checkptLk.Unlock()

	ts, err := syncer.ChainStore().LoadTipSet(ctx, tsk)
	if err != nil {
		return xerrors.Errorf("cannot find tipset: %w", err)
	}

	hts := syncer.ChainStore().GetHeaviestTipSet()
	anc, err := syncer.ChainStore().IsAncestorOf(ctx, ts, hts)
	if err != nil {
		return xerrors.Errorf("cannot determine whether checkpoint tipset is in main-chain: %w", err)
	}

	if !hts.Equals(ts) && !anc {
		return xerrors.Errorf("cannot mark tipset as checkpoint, since it isn't in the main-chain: %w", err)
	}

	tskBytes, err := json.Marshal(tsk)
	if err != nil {
		return err
	}

	err = syncer.ds.Put(ctx, CheckpointKey, tskBytes)
	if err != nil {
		return err
	}

	syncer.checkpt = tsk

	return nil
}

func (syncer *Syncer) GetCheckpoint() types.TipSetKey {
	syncer.checkptLk.Lock()
	defer syncer.checkptLk.Unlock()
	return syncer.checkpt
}
