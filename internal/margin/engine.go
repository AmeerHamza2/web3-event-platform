package margin

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Margin status, ordered by severity.
const (
	StatusHealthy      = "healthy"
	StatusMarginCall   = "margin_call"
	StatusLiquidatable = "liquidatable"
)

// Asset describes a collateral asset the engine understands.
type Asset struct {
	Symbol           string
	Token            common.Address
	Decimals         uint8    // token decimals (e.g. 18 for WETH, 6 for USDC)
	CollateralFactor *big.Rat // haircut in [0,1]; borrowing power = value * factor
	PriceFeed        common.Address
}

// Config parameterises the engine. HealthFactor = weighted collateral / debt.
// Below LiquidationHF a portfolio can be liquidated; between that and MarginCallHF
// it is a margin call; above, healthy.
type Config struct {
	Assets        []Asset
	MarginCallHF  *big.Rat // e.g. 1.15
	LiquidationHF *big.Rat // e.g. 1.00
}

// Engine computes portfolio margin for an account.
type Engine struct {
	reader BalanceReader
	oracle PriceOracle
	cfg    Config
}

func NewEngine(reader BalanceReader, oracle PriceOracle, cfg Config) *Engine {
	return &Engine{reader: reader, oracle: oracle, cfg: cfg}
}

// Position is one valued holding in the report.
type Position struct {
	Symbol           string `json:"symbol"`
	BalanceRaw       string `json:"balance_raw"`
	PriceUSD         string `json:"price_usd"`
	ValueUSD         string `json:"value_usd"`
	CollateralFactor string `json:"collateral_factor"`
	WeightedUSD      string `json:"weighted_usd"`
}

// Report is the margin evaluation for an account.
type Report struct {
	Account               string     `json:"account"`
	GrossCollateralUSD    string     `json:"gross_collateral_usd"`
	WeightedCollateralUSD string     `json:"weighted_collateral_usd"`
	DebtUSD               string     `json:"debt_usd"`
	HealthFactor          string     `json:"health_factor"` // "n/a" when there is no debt
	Status                string     `json:"status"`
	Positions             []Position `json:"positions"`
}

// Evaluate values the account's holdings, applies collateral factors, and
// computes the health factor and margin status against debtUSD.
func (e *Engine) Evaluate(ctx context.Context, account common.Address, debtUSD *big.Rat) (Report, error) {
	gross := new(big.Rat)
	weighted := new(big.Rat)
	positions := make([]Position, 0, len(e.cfg.Assets))

	for _, a := range e.cfg.Assets {
		bal, err := e.reader.BalanceOf(ctx, a.Token, account)
		if err != nil {
			return Report{}, fmt.Errorf("balance %s: %w", a.Symbol, err)
		}
		if bal.Sign() == 0 {
			continue // skip empty holdings
		}
		price, err := e.oracle.Price(ctx, a.PriceFeed)
		if err != nil {
			return Report{}, fmt.Errorf("price %s: %w", a.Symbol, err)
		}

		// value = (balance / 10^decimals) * priceUSD
		balRat := new(big.Rat).SetFrac(bal, pow10(a.Decimals))
		value := new(big.Rat).Mul(balRat, price.Rat())
		w := new(big.Rat).Mul(value, a.CollateralFactor)

		gross.Add(gross, value)
		weighted.Add(weighted, w)

		positions = append(positions, Position{
			Symbol:           a.Symbol,
			BalanceRaw:       bal.String(),
			PriceUSD:         price.Rat().FloatString(2),
			ValueUSD:         value.FloatString(2),
			CollateralFactor: a.CollateralFactor.FloatString(2),
			WeightedUSD:      w.FloatString(2),
		})
	}

	report := Report{
		Account:               account.Hex(),
		GrossCollateralUSD:    gross.FloatString(2),
		WeightedCollateralUSD: weighted.FloatString(2),
		DebtUSD:               debtUSD.FloatString(2),
		Positions:             positions,
	}

	// No debt → no liquidation risk; health factor is undefined.
	if debtUSD.Sign() <= 0 {
		report.HealthFactor = "n/a"
		report.Status = StatusHealthy
		return report, nil
	}

	hf := new(big.Rat).Quo(weighted, debtUSD)
	report.HealthFactor = hf.FloatString(4)
	report.Status = e.status(hf)
	return report, nil
}

func (e *Engine) status(hf *big.Rat) string {
	switch {
	case hf.Cmp(e.cfg.LiquidationHF) < 0:
		return StatusLiquidatable
	case hf.Cmp(e.cfg.MarginCallHF) < 0:
		return StatusMarginCall
	default:
		return StatusHealthy
	}
}
