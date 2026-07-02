package main

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/AmeerHamza2/web3-event-platform/internal/margin"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

// assetJSON is the wire form of a configured collateral asset (ASSETS_JSON).
type assetJSON struct {
	Symbol           string `json:"symbol"`
	Token            string `json:"token"`
	Decimals         uint8  `json:"decimals"`
	CollateralFactor string `json:"collateral_factor"` // decimal in [0,1], e.g. "0.8"
	PriceFeed        string `json:"price_feed"`
}

func main() {
	log := logging.New("margin")

	addr := server.EnvOr("MARGIN_ADDR", ":8085")
	rpcURL := server.EnvOr("ETH_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		log.Error("dial ethereum rpc", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	reader, err := margin.NewChainBalanceReader(client)
	if err != nil {
		log.Error("init balance reader", "error", err)
		os.Exit(1)
	}
	oracle, err := margin.NewChainlinkOracle(client, 8) // Chainlink USD feeds use 8 decimals
	if err != nil {
		log.Error("init oracle", "error", err)
		os.Exit(1)
	}

	assets, err := loadAssets(os.Getenv("ASSETS_JSON"))
	if err != nil {
		log.Error("parse ASSETS_JSON", "error", err)
		os.Exit(1)
	}
	if len(assets) == 0 {
		log.Warn("no ASSETS_JSON configured; every account will value to zero collateral")
	}

	engine := margin.NewEngine(reader, oracle, margin.Config{
		Assets:        assets,
		MarginCallHF:  big.NewRat(115, 100),
		LiquidationHF: big.NewRat(100, 100),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /margin/{address}", marginHandler(engine, log))

	handler := httpx.Chain(mux, httpx.RequestID, httpx.Logger(log), httpx.Recovery(log))
	server.Run(log, addr, handler)
}

func marginHandler(engine *margin.Engine, log interface{ Warn(string, ...any) }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")
		if !common.IsHexAddress(address) {
			httpx.Error(w, http.StatusBadRequest, "invalid address")
			return
		}
		// Debt is supplied by the caller here; a fuller build would read it from
		// the lending protocols the account borrows from.
		debt := new(big.Rat)
		if d := r.URL.Query().Get("debt_usd"); d != "" {
			if _, ok := debt.SetString(d); !ok {
				httpx.Error(w, http.StatusBadRequest, "invalid debt_usd")
				return
			}
		}

		report, err := engine.Evaluate(r.Context(), common.HexToAddress(address), debt)
		if err != nil {
			log.Warn("evaluate failed", "error", err)
			httpx.Error(w, http.StatusBadGateway, "could not evaluate margin (chain read failed)")
			return
		}
		httpx.JSON(w, http.StatusOK, report)
	}
}

func loadAssets(raw string) ([]margin.Asset, error) {
	if raw == "" {
		return nil, nil
	}
	var in []assetJSON
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, err
	}
	out := make([]margin.Asset, 0, len(in))
	for _, a := range in {
		factor := new(big.Rat)
		if _, ok := factor.SetString(a.CollateralFactor); !ok {
			return nil, &parseError{field: "collateral_factor", value: a.CollateralFactor}
		}
		out = append(out, margin.Asset{
			Symbol:           a.Symbol,
			Token:            common.HexToAddress(a.Token),
			Decimals:         a.Decimals,
			CollateralFactor: factor,
			PriceFeed:        common.HexToAddress(a.PriceFeed),
		})
	}
	return out, nil
}

type parseError struct{ field, value string }

func (e *parseError) Error() string { return "invalid " + e.field + ": " + e.value }
