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
	log.Infow("ExecCommit2WithSupra Start", "sector", storiface.SectorName(sector.ID))
	defer log.Infow("ExecCommit2WithSupra Finish", "sector", storiface.SectorName(sector.ID))

	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("s-%v-%v", sector.ID.Miner.String(), sector.ID.Number.String()))
	if err != nil {
		return nil, err
	}
	c1outPath := filepath.Join(tmpDir, "c1.out")
	if err = os.WriteFile(c1outPath, phase1Out, 0666); err != nil {
		return nil, err
	}
	c2outPath := filepath.Join(tmpDir, "c2.out")

	proverID, err := toProverID(sector.ID.Miner)
	if err != nil {
		return nil, err
	}

	program := "./supra-c2"
	cmd := exec.CommandContext(ctx, program,
		"--sector-id", sector.ID.Number.String(),
		"--prover-id", proverID,
		"--input-file", c1outPath,
		"--output-file", c2outPath,
	)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return os.ReadFile(c2outPath)
}

func toProverID(minerID abi.ActorID) (string, error) {
	maddr, err := address.NewIDAddress(uint64(minerID))
	if err != nil {
		return "", errors.New("failed to convert ActorID to prover id")
	}
	data := [32]byte{}
	for i, b := range maddr.Payload() {
		if i >= 32 {
			break
		}
		data[i] = b
	}
	return hex.EncodeToString(data[:]), nil
}