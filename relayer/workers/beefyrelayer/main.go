package beefyrelayer

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/snowfork/go-substrate-rpc-client/v2/types"
	"github.com/snowfork/polkadot-ethereum/relayer/chain"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/ethereum"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/relaychain"
	"github.com/snowfork/polkadot-ethereum/relayer/crypto/secp256k1"
	"github.com/snowfork/polkadot-ethereum/relayer/workers/beefyrelayer/store"
)

type Worker struct {
	relaychainConfig        *relaychain.Config
	ethereumConfig          *ethereum.Config
	relaychainConn          *relaychain.Connection
	beefyEthereumListener   *BeefyEthereumListener
	beefyRelaychainListener *BeefyRelaychainListener
	ethereumConn            *ethereum.Connection
	beefyEthereumWriter     *BeefyEthereumWriter
	log                     *logrus.Entry
	beefyDB                 *store.Database
	beefyMessages           chan store.BeefyRelayInfo
	ethHeaders              chan chain.Header
}

const Name = "beefy-relayer"

func NewWorker(relaychainConfig *relaychain.Config, ethereumConfig *ethereum.Config, dbConfig *store.Config) (*Worker, error) {
	log := logrus.WithField("beefy-relayer", Name)

	fmt.Println("Creating beefy-relayer")

	db, err := store.PrepareDatabase(dbConfig)
	if err != nil {
		return nil, err
	}

	dbMessages := make(chan store.DatabaseCmd)
	logger := log.WithField("database", "Beefy")
	beefyDB := store.NewDatabase(db, dbMessages, logger)

	ethereumKeypair, err := secp256k1.NewKeypairFromString(ethereumConfig.PrivateKey)
	if err != nil {
		return nil, err
	}

	relaychainConn := relaychain.NewConnection(relaychainConfig.Endpoint, log)
	ethereumConn := ethereum.NewConnection(ethereumConfig.Endpoint, ethereumKeypair, log)

	beefyMessages := make(chan store.BeefyRelayInfo)
	ethHeaders := make(chan chain.Header)

	beefyEthereumListener, err := NewBeefyEthereumListener(ethereumConfig,
		ethereumConn, beefyDB, beefyMessages, dbMessages, ethHeaders,
		log)
	if err != nil {
		return nil, err
	}

	beefyEthereumWriter, err := NewBeefyEthereumWriter(ethereumConfig, ethereumConn,
		beefyDB, dbMessages, beefyMessages, log)
	if err != nil {
		return nil, err
	}

	beefyRelaychainListener := NewBeefyRelaychainListener(
		relaychainConfig,
		relaychainConn,
		beefyMessages,
		log,
	)

	return &Worker{
		relaychainConfig:        relaychainConfig,
		ethereumConfig:          ethereumConfig,
		relaychainConn:          relaychainConn,
		beefyEthereumListener:   beefyEthereumListener,
		ethereumConn:            ethereumConn,
		beefyEthereumWriter:     beefyEthereumWriter,
		beefyRelaychainListener: beefyRelaychainListener,
		log:                     log,
		beefyDB:                 beefyDB,
		beefyMessages:           beefyMessages,
		ethHeaders:              ethHeaders,
	}, nil
}

func (worker *Worker) Start(ctx context.Context, eg *errgroup.Group) error {
	fmt.Println("Starting beefy-relayer")

	err := worker.beefyDB.Start(ctx, eg)
	if err != nil {
		worker.log.WithFields(log.Fields{
			"database": "Beefy",
			"error":    err,
		}).Error("Failed to start database")
		return err
	}
	worker.log.WithField("database", "Beefy").Info("Started database")

	if worker.beefyEthereumListener == nil ||
		worker.beefyEthereumWriter == nil ||
		worker.beefyRelaychainListener == nil {
		return fmt.Errorf("Sender needs to be set before starting chain")
	}

	err = worker.relaychainConn.Connect(ctx)
	if err != nil {
		return err
	}

	err = worker.beefyRelaychainListener.Start(ctx, eg)
	if err != nil {
		return err
	}

	err = worker.ethereumConn.Connect(ctx)
	if err != nil {
		return err
	}

	eg.Go(func() error {

		err = worker.beefyEthereumListener.Start(ctx, eg, uint64(worker.ethereumConfig.DescendantsUntilFinal))
		if err != nil {
			return err
		}

		err = worker.beefyEthereumWriter.Start(ctx, eg)
		if err != nil {
			return err
		}

		return nil
	})

	return nil
}

func (worker *Worker) Stop() {
	if worker.relaychainConn != nil {
		worker.relaychainConn.Close()
	}
	if worker.ethereumConn != nil {
		worker.ethereumConn.Close()
	}
	if worker.beefyDB != nil {
		worker.beefyDB.Stop()
	}
}

func (worker *Worker) Name() string {
	return Name
}

func (worker *Worker) QueryCurrentEpoch() error {
	worker.log.Info("Creating storage key...")

	storageKey, err := types.CreateStorageKey(worker.relaychainConn.GetMetadata(), "Babe", "Epoch", nil, nil)
	if err != nil {
		return err
	}

	worker.log.Info("Attempting to query current epoch...")

	// var headerID ethereum.HeaderID
	var epochData interface{}
	_, err = worker.relaychainConn.GetAPI().RPC.State.GetStorageLatest(storageKey, &epochData)
	if err != nil {
		return err
	}

	worker.log.Info("Retrieved current epoch data:", epochData)

	// nextHeaderID := ethereum.HeaderID{Number: types.NewU64(uint64(headerID.Number) + 1)}
	// return &nextHeaderID, nil

	return nil
}
