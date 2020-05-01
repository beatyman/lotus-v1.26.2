package lp2p

import (
	"context"
	"time"

	host "github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
)

func init() {
	// configure larger overlay parameters
	pubsub.GossipSubD = 8
	pubsub.GossipSubDscore = 6
	pubsub.GossipSubDlo = 6
	pubsub.GossipSubDhi = 12
	pubsub.GossipSubDlazy = 12
}

type PubsubOpt func(host.Host) pubsub.Option

func PubsubTracer() PubsubOpt {
	return func(host host.Host) pubsub.Option {
		pi, err := peer.AddrInfoFromP2pAddr(ma.StringCast("/ip4/147.75.67.199/tcp/4001/p2p/QmTd6UvR47vUidRNZ1ZKXHrAFhqTJAD27rKL9XYghEKgKX"))
		if err != nil {
			panic(err)
		}

		tr, err := pubsub.NewRemoteTracer(context.TODO(), host, *pi)
		if err != nil {
			panic(err)
		}

		return pubsub.WithEventTracer(tr)
	}
}

func GossipSub(pubsubOptions ...PubsubOpt) interface{} {
	return func(mctx helpers.MetricsCtx, lc fx.Lifecycle, host host.Host, nn dtypes.NetworkName, bp dtypes.BootstrapPeers) (service *pubsub.PubSub, err error) {
		bootstrappers := make(map[peer.ID]struct{})
		for _, pi := range bp {
			bootstrappers[pi.ID] = struct{}{}
		}
		_, isBootstrapNode := bootstrappers[host.ID()]

		v11Options := []pubsub.Option{
			// Gossipsubv1.1 configuration
			pubsub.WithFloodPublish(true),
			pubsub.WithPeerScore(
				&pubsub.PeerScoreParams{
					AppSpecificScore: func(p peer.ID) float64 {
						// return a heavy positive score for bootstrappers so that we don't unilaterally prune
						// them and accept PX from them.
						// we don't do that in the bootstrappers themselves to avoid creating a closed mesh
						// between them (however we might want to consider doing just that)
						_, ok := bootstrappers[p]
						if ok && !isBootstrapNode {
							return 2500
						}

						// TODO: we want to  plug the application specific score to the node itself in order
						//       to provide feedback to the pubsub system based on observed behaviour
						return 0
					},
					AppSpecificWeight: 1,

					// This sets the IP colocation threshold to 1 peer per
					IPColocationFactorThreshold: 1,
					IPColocationFactorWeight:    -100,
					// TODO we want to whitelist IPv6 /64s that belong to datacenters etc
					// IPColocationFactorWhitelist: map[string]struct{}{},

					DecayInterval: pubsub.DefaultDecayInterval,
					DecayToZero:   pubsub.DefaultDecayToZero,

					// this retains non-positive scores for 6 hours
					RetainScore: 6 * time.Hour,

					// topic parameters
					Topics: map[string]*pubsub.TopicScoreParams{
						build.BlocksTopic(nn): {
							// expected 10 blocks/min
							TopicWeight: 0.1, // max is 50, max mesh penalty is -10, single invalid message is -100

							// 1 tick per second, maxes at 1 after 1 hour
							TimeInMeshWeight:  0.00027, // ~1/3600
							TimeInMeshQuantum: time.Second,
							TimeInMeshCap:     1,

							// deliveries decay after 1 hour, cap at 100 blocks
							FirstMessageDeliveriesWeight: 5, // max value is 500
							FirstMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
							FirstMessageDeliveriesCap:    100, // 100 blocks in an hour

							// tracks deliveries in the last minute
							// penalty activates at 1 minute and expects ~0.4 blocks
							MeshMessageDeliveriesWeight:     -576, // max penalty is -100
							MeshMessageDeliveriesDecay:      pubsub.ScoreParameterDecay(time.Minute),
							MeshMessageDeliveriesCap:        10,      // 10 blocks in a minute
							MeshMessageDeliveriesThreshold:  0.41666, // 10/12/2 blocks/min
							MeshMessageDeliveriesWindow:     10 * time.Millisecond,
							MeshMessageDeliveriesActivation: time.Minute,

							// decays after 15 min
							MeshFailurePenaltyWeight: -576,
							MeshFailurePenaltyDecay:  pubsub.ScoreParameterDecay(15 * time.Minute),

							// invalid messages decay after 1 hour
							InvalidMessageDeliveriesWeight: -1000,
							InvalidMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
						},
						build.MessagesTopic(nn): {
							// expected > 1 tx/second
							TopicWeight: 0.05, // max is 25, max mesh penalty is -5, single invalid message is -100

							// 1 tick per second, maxes at 1 hour
							TimeInMeshWeight:  0.0002778, // ~1/3600
							TimeInMeshQuantum: time.Second,
							TimeInMeshCap:     1,

							// deliveries decay after 10min, cap at 1000 tx
							FirstMessageDeliveriesWeight: 0.5, // max value is 500
							FirstMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(10 * time.Minute),
							FirstMessageDeliveriesCap:    1000,

							// tracks deliveries in the last minute
							// penalty activates at 1 min and expects 2.5 txs
							MeshMessageDeliveriesWeight:     -16, // max penalty is -100
							MeshMessageDeliveriesDecay:      pubsub.ScoreParameterDecay(time.Minute),
							MeshMessageDeliveriesCap:        100, // 100 txs in a minute
							MeshMessageDeliveriesThreshold:  2.5, // 60/12/2 txs/minute
							MeshMessageDeliveriesWindow:     10 * time.Millisecond,
							MeshMessageDeliveriesActivation: time.Minute,

							// decays after 5min
							MeshFailurePenaltyWeight: -16,
							MeshFailurePenaltyDecay:  pubsub.ScoreParameterDecay(5 * time.Minute),

							// invalid messages decay after 1 hour
							InvalidMessageDeliveriesWeight: -2000,
							InvalidMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
						},
					},
				},
				&pubsub.PeerScoreThresholds{
					GossipThreshold:             -500,
					PublishThreshold:            -1000,
					GraylistThreshold:           -2500,
					AcceptPXThreshold:           1000,
					OpportunisticGraftThreshold: 2.5,
				},
			),
		}

		// enable Peer eXchange on bootstrappers
		if isBootstrapNode {
			v11Options = append(v11Options, pubsub.WithPeerExchange(true))
		}

		// TODO: we want to hook the peer score inspector so that we can gain visibility
		//       in peer scores for debugging purposes -- this might be trigged by metrics collection
		// v11Options = append(v11Options, pubsub.WithPeerScoreInspect(XXX, time.Second))

		options := append(v11Options, paresOpts(host, pubsubOptions)...)

		return pubsub.NewGossipSub(helpers.LifecycleCtx(mctx, lc), host, options...)
	}
}

func paresOpts(host host.Host, in []PubsubOpt) []pubsub.Option {
	out := make([]pubsub.Option, len(in))
	for k, v := range in {
		out[k] = v(host)
	}
	return out
}
