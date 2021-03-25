// Copyright 2020 Snowfork
// SPDX-License-Identifier: LGPL-3.0-only

package relaychain

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/snowfork/go-substrate-rpc-client/v2/types"

	"github.com/snowfork/polkadot-ethereum/relayer/chain"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/ethereum"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/ethereum/syncer"
	"github.com/snowfork/polkadot-ethereum/relayer/contracts/polkadotrelaychainbridge"
	"github.com/snowfork/polkadot-ethereum/relayer/relaychain"
	chainTypes "github.com/snowfork/polkadot-ethereum/relayer/substrate"
)

type Listener struct {
	config   *Config
	conn     *Connection
	econn    *ethereum.Connection
	contract *polkadotrelaychainbridge.Contract
	messages chan<- []chain.Message
	beefy    chan relaychain.BeefyCommitmentInfo
	log      *logrus.Entry
}

func NewListener(config *Config, conn *Connection, econn *ethereum.Connection, messages chan<- []chain.Message,
	beefy chan relaychain.BeefyCommitmentInfo, log *logrus.Entry) *Listener {
	return &Listener{
		config:   config,
		conn:     conn,
		econn:    econn,
		messages: messages,
		beefy:    beefy,
		log:      log,
	}
}

func (li *Listener) Start(ctx context.Context, eg *errgroup.Group, initBlockHeight uint64, descendantsUntilFinal uint64) error {
	li.log.Info("Starting Relaychain Listener")

	contract, err := polkadotrelaychainbridge.NewContract(common.HexToAddress(li.config.Ethereum.Contracts.RelayBridgeLightClient), li.econn.GetClient())
	if err != nil {
		return err
	}
	li.contract = contract

	eg.Go(func() error {
		return li.subBeefyJustifications(ctx)
	})

	// Ethereum facing information
	hcs, err := ethereum.NewHeaderCacheState(
		eg,
		initBlockHeight,
		&ethereum.DefaultBlockLoader{Conn: li.econn},
		nil,
	)
	if err != nil {
		return err
	}

	eg.Go(func() error {
		return li.pollEthereumBlocks(ctx, initBlockHeight, 0, hcs)
	})

	// eg.Go(func() error {
	// 	return li.pollLightBridgeEvents(ctx, initBlockHeight, descendantsUntilFinal, hcs)
	// })

	return nil
}

func (li *Listener) onDone(ctx context.Context) error {
	li.log.Info("Shutting down listener...")
	close(li.messages)
	return ctx.Err()
}

func (li *Listener) subBeefyJustifications(ctx context.Context) error {
	ch := make(chan interface{})

	sub, err := li.conn.api.Client.Subscribe(context.Background(), "beefy", "subscribeJustifications", "unsubscribeJustifications", "justifications", ch)
	if err != nil {
		panic(err)
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return li.onDone(ctx)
		case msg := <-ch:

			signedCommitment := &relaychain.SignedCommitment{}
			err := types.DecodeFromHexString(msg.(string), signedCommitment)
			if err != nil {
				li.log.WithError(err).Error("Failed to decode beefy commitment messages")
			}

			li.log.Info("Relaychain Listener witnessed a new BEEFY commitment: \n", msg.(string))

			if len(signedCommitment.Signatures) == 0 {
				li.log.Info("BEEFY commitment has no signatures, skipping...")
				continue
			}

			type Authorities = [][33]uint8

			blockHash, err := li.conn.api.RPC.Chain.GetBlockHash(uint64(signedCommitment.Commitment.BlockNumber))
			if err != nil {
				panic(err)
			}

			meta, err := li.conn.api.RPC.State.GetMetadataLatest()
			if err != nil {
				panic(err)
			}

			storageKey, err := types.CreateStorageKey(meta, "Beefy", "Authorities", nil, nil)
			if err != nil {
				panic(err)
			}

			storageChangeSet, err := li.conn.api.RPC.State.QueryStorage([]types.StorageKey{storageKey}, blockHash, blockHash)
			if err != nil {
				li.log.WithError(err).Error("Failed to read authorities from storage")
				// sleep(ctx, retryInterval)
				continue
			}

			authorities := Authorities{}
			for _, storageChange := range storageChangeSet {
				for _, keyValueOption := range storageChange.Changes {
					bz, err := keyValueOption.MarshalJSON()
					if err != nil {
						panic(err)
					}

					fmt.Println("attempting to decode bz")
					err = types.DecodeFromBytes(bz, authorities)
					if err != nil {
						panic(err)
					}

				}
			}

			fmt.Println("authorities:", authorities)
			// TODO: Decode authorities using @polkadot/util-crypto/ethereum/encode.js ethereumEncode() method

			// if data != nil {
			// 	li.log.WithFields(logrus.Fields{
			// 		"block":               signedCommitment.Commitment.BlockNumber,
			// 		"commitmentSizeBytes": len(*data),
			// 	}).Debug("Retrieved authorities from storage")
			// } else {
			// 	li.log.WithError(err).Error("Authorities not found in storage")
			// 	continue
			// }

			beefyValidatorAddresses := []common.Address{
				common.HexToAddress("0xE04CC55ebEE1cBCE552f250e85c57B70B2E2625b"),
				common.HexToAddress("0x25451A4de12dcCc2D166922fA938E900fCc4ED24"),
			}

			beefyCommitmentInfo := relaychain.NewBeefyCommitmentInfo(beefyValidatorAddresses, signedCommitment)

			li.messages <- []chain.Message{beefyCommitmentInfo}
		}
	}
}

func sleep(ctx context.Context, delay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func getAuxiliaryDigestItem(digest types.Digest) (*chainTypes.AuxiliaryDigestItem, error) {
	for _, digestItem := range digest {
		if digestItem.IsOther {
			var auxDigestItem chainTypes.AuxiliaryDigestItem
			err := types.DecodeFromBytes(digestItem.AsOther, &auxDigestItem)
			if err != nil {
				return nil, err
			}
			return &auxDigestItem, nil
		}
	}
	return nil, nil
}

// pollEthereumBlocks transitions BEEFY commitments from InitialVerificationTxConfirmed to ReadyToComplete status
func (li *Listener) pollEthereumBlocks(
	ctx context.Context,
	initBlockHeight uint64,
	descendantsUntilFinal uint64,
	hcs *ethereum.HeaderCacheState,
) error {
	headers := make(chan *gethTypes.Header, 5)
	headerEg, headerCtx := errgroup.WithContext(ctx)

	headerSyncer := syncer.NewSyncer(descendantsUntilFinal, syncer.NewHeaderLoader(li.econn.GetClient()), headers, li.log)

	li.log.Info("Syncing headers starting...")
	err := headerSyncer.StartSync(headerCtx, headerEg, initBlockHeight-1)
	if err != nil {
		li.log.WithError(err).Error("Failed to start header sync")
		return err
	}

	li.log.Info("Headers synced!")

	for {
		select {
		case <-ctx.Done():
			return li.onDone(ctx)
		case <-headerCtx.Done():
			return li.onDone(ctx)
		case gethheader := <-headers:
			li.log.Info("Relaychain Listener pollEthereumBlocks() received a new header")

			blockNumber := gethheader.Number.Uint64()
			for beefyCommitment := range li.beefy {
				if beefyCommitment.Status == relaychain.InitialVerificationTxConfirmed {
					if beefyCommitment.CompleteOnBlock >= blockNumber {
						li.log.Info("pollEthereumBlocks marked BEEFY commitment ReadyToComplete")

						beefyCommitment.Status = relaychain.ReadyToComplete
						li.messages <- []chain.Message{beefyCommitment}
					}
				}
			}
		}
	}
}

// pollLightBridgeEvents fetches events from the PolkadotRelayChainBridge every block
func (li *Listener) pollLightBridgeEvents(ctx context.Context) error {
	headers := make(chan *gethTypes.Header, 5)
	_, headerCtx := errgroup.WithContext(ctx)

	li.log.Info("Starting pollLightBridgeEvents()")

	for {
		select {
		case <-ctx.Done():
			return li.onDone(ctx)
		case <-headerCtx.Done():
			return li.onDone(ctx)
		case gethheader := <-headers:
			li.log.Info("Relaychain Listener pollLightBridgeEvents() received a new header")

			if li.beefy == nil {
				li.log.Info("Not polling block details since channel is nil")
				continue
			}

			blockNumber := gethheader.Number.Uint64()

			// Query ContractInitialVerificationSuccessful events
			var events []*polkadotrelaychainbridge.ContractInitialVerificationSuccessful
			contractEvents, err := li.queryEvents(ctx, li.contract, blockNumber, &blockNumber)
			if err != nil {
				li.log.WithError(err).Error("Failure fetching event logs")
			}
			events = append(events, contractEvents...)

			li.log.Info("pollEthereumBlocks found events: ", len(events))
			li.processEvents(ctx, events)
		}
	}
}

// queryEvents queries ContractInitialVerificationSuccessful events from the PolkadotRelayChainBridge contract
func (li *Listener) queryEvents(ctx context.Context, contract *polkadotrelaychainbridge.Contract, start uint64,
	end *uint64) ([]*polkadotrelaychainbridge.ContractInitialVerificationSuccessful, error) {
	var events []*polkadotrelaychainbridge.ContractInitialVerificationSuccessful
	filterOps := bind.FilterOpts{Start: start, End: end, Context: ctx}

	iter, err := contract.FilterInitialVerificationSuccessful(&filterOps)
	if err != nil {
		return nil, err
	}

	for {
		more := iter.Next()
		if !more {
			err = iter.Error()
			if err != nil {
				return nil, err
			}
			break
		}

		events = append(events, iter.Event)
	}

	return events, nil
}

// processEvents matches events to BEEFY commitment info by transaction hash
func (li *Listener) processEvents(ctx context.Context, events []*polkadotrelaychainbridge.ContractInitialVerificationSuccessful) {
	for _, event := range events {
		for beefyCommitment := range li.beefy {
			if beefyCommitment.Status == relaychain.InitialVerificationTxSent {
				if beefyCommitment.InitialVerificationTxHash.Hex() == event.Raw.TxHash.Hex() {
					beefyCommitment.Status = relaychain.InitialVerificationTxConfirmed
					beefyCommitment.CompleteOnBlock = event.Raw.BlockNumber + li.config.Ethereum.BeefyBlockDelay
				}
			}
			li.beefy <- beefyCommitment // TODO: do we need any additional event info?
		}
	}
}
