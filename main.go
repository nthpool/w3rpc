package main

import (
	"fmt"
	"net/http"
	"time"

	_ "net/http/pprof"

	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/logger"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/lmittmann/w3"
	"github.com/nthpool/w3rpc/bundleapi"
	"github.com/nthpool/w3rpc/fetcher"
	"github.com/nthpool/w3rpc/w3backend"
	"github.com/rs/zerolog/log"
)

var (
	client     = w3.MustDial("http://18.206.227.185:8545")
	rpcFetcher = fetcher.NewRPCFetcher(client, nil)
	kvDB       = w3backend.NewBadgerKV()
	backend    = w3backend.NewW3Backend(client, w3backend.NewDB(rpcFetcher, kvDB))
)

type handler struct {
	rpc *rpc.Server
}

func (h *handler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.rpc.ServeHTTP(res, req)
}

func updateState() {
	log.Info().Str("key", "updateState").Msg("subscribed to state stream")
	for {
		select {
		case s := <-rpcFetcher.StateStream():
			ts := time.Now()
			err := kvDB.ProcessState(s.TraceBlock)
			backend.SetHead(s.Block)
			if err != nil {
				log.Err(err).Send()
			}
			log.Info().Str("key", "updateState").Int("txs", len(s.TraceBlock)).TimeDiff("rt", time.Now(), ts).Send()
		}
	}
}

func main() {
	go updateState()
	tracingApi := []rpc.API{
		{
			Namespace: "debug",
			Service:   tracers.NewAPI(backend),
		},
		{
			Namespace: "eth",
			Service:   bundleapi.NewBundleAPI(backend),
		},
	}
	srv := rpc.NewServer()
	// Register all the APIs exposed by the services
	for _, api := range tracingApi {
		if err := srv.RegisterName(api.Namespace, api.Service); err != nil {
			return
		}
	}
	go func() {
		log.Info().Err(http.ListenAndServe("localhost:6060", nil)).Send()
	}()
	err := http.ListenAndServe("localhost:8080", &handler{rpc: srv})
	if err != nil {
		panic(err)
	}
	fmt.Println("test")
}
