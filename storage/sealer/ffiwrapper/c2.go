package ffiwrapper

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func ExecCommit2WithSupra(ctx context.Context, sector storiface.SectorRef, phase1Out storiface.Commit1Out) (storiface.Proof, error) {
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("s-%v-%v", sector.ID.Miner.String(), sector.ID.Number.String()))
	if err != nil {
		return nil, err
	}
	c1outPath := filepath.Join(tmpDir, "c1.out")
	if err = os.WriteFile(c1outPath, phase1Out, 0666); err != nil {
		return nil, err
	}
	c2outPath := filepath.Join(tmpDir, "c2.out")

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, err
	}
	proverID, err := toProverID(sector.ID.Miner)
	if err != nil {
		return nil, err
	}

	program := "./supra-c2"
	cmd := exec.CommandContext(ctx, program,
		"--sector-id", sector.ID.Number.String(),
		"--sector-size", ssize.ShortString(),
		"--prover-id", hex.EncodeToString(proverID),
		"--proof-type", fmt.Sprintf("%v", int64(sector.ProofType)),
		"--input-file", c1outPath,
		"--output-file", c2outPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return os.ReadFile(c2outPath)
}

func toProverID(minerID abi.ActorID) ([]byte, error) {
	maddr, err := address.NewIDAddress(uint64(minerID))
	if err != nil {
		return nil, errors.New("failed to convert ActorID to prover id")
	}
	return maddr.Payload(), nil
}
