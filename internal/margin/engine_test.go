package margin

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

var (
	weth     = common.HexToAddress("0x0000000000000000000000000000000000000001")
	wethFeed = common.HexToAddress("0x00000000000000000000000000000000000000f1")
	usdc     = common.HexToAddress("0x0000000000000000000000000000000000000002")
	usdcFeed = common.HexToAddress("0x00000000000000000000000000000000000000f2")
	acct     = common.HexToAddress("0x00000000000000000000000000000000000000aa")
)

// usdPrice builds a Chainlink-style USD price (8 decimals).
func usdPrice(dollars int64) Price {
	return Price{Value: new(big.Int).Mul(big.NewInt(dollars), pow10(8)), Decimals: 8}
}

func testEngine(balances StaticBalances, factorWETH, factorUSDC *big.Rat) *Engine {
	oracle := StaticOracle{wethFeed: usdPrice(2000), usdcFeed: usdPrice(1)}
	cfg := Config{
		Assets: []Asset{
			{Symbol: "WETH", Token: weth, Decimals: 18, CollateralFactor: factorWETH, PriceFeed: wethFeed},
			{Symbol: "USDC", Token: usdc, Decimals: 6, CollateralFactor: factorUSDC, PriceFeed: usdcFeed},
		},
		MarginCallHF:  big.NewRat(115, 100),
		LiquidationHF: big.NewRat(1, 1),
	}
	return NewEngine(balances, oracle, cfg)
}

func oneWETH() StaticBalances { return StaticBalances{weth: pow10(18)} } // 1 WETH

func TestValuationAndHealthy(t *testing.T) {
	e := testEngine(oneWETH(), big.NewRat(8, 10), big.NewRat(9, 10))
	r, err := e.Evaluate(context.Background(), acct, big.NewRat(1000, 1))
	if err != nil {
		t.Fatal(err)
	}
	// 1 WETH * $2000 = 2000 gross; * 0.8 = 1600 weighted; HF = 1600/1000 = 1.6.
	if r.GrossCollateralUSD != "2000.00" || r.WeightedCollateralUSD != "1600.00" {
		t.Fatalf("valuation: gross=%s weighted=%s", r.GrossCollateralUSD, r.WeightedCollateralUSD)
	}
	if r.HealthFactor != "1.6000" || r.Status != StatusHealthy {
		t.Fatalf("HF=%s status=%s, want 1.6000 healthy", r.HealthFactor, r.Status)
	}
}

func TestMultiAssetWithDecimals(t *testing.T) {
	bal := StaticBalances{
		weth: pow10(18),                                    // 1 WETH  -> $2000
		usdc: new(big.Int).Mul(big.NewInt(1000), pow10(6)), // 1000 USDC -> $1000
	}
	e := testEngine(bal, big.NewRat(8, 10), big.NewRat(9, 10))
	r, err := e.Evaluate(context.Background(), acct, big.NewRat(2000, 1))
	if err != nil {
		t.Fatal(err)
	}
	// gross 3000; weighted 1600 + 900 = 2500; HF = 2500/2000 = 1.25.
	if r.GrossCollateralUSD != "3000.00" || r.WeightedCollateralUSD != "2500.00" {
		t.Fatalf("gross=%s weighted=%s", r.GrossCollateralUSD, r.WeightedCollateralUSD)
	}
	if r.HealthFactor != "1.2500" || r.Status != StatusHealthy {
		t.Fatalf("HF=%s status=%s", r.HealthFactor, r.Status)
	}
	if len(r.Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(r.Positions))
	}
}

func TestStatusBoundaries(t *testing.T) {
	// weighted is fixed at 1600 (1 WETH, factor 0.8). Vary debt to hit exact HFs.
	e := testEngine(oneWETH(), big.NewRat(8, 10), big.NewRat(9, 10))
	cases := []struct {
		name string
		debt *big.Rat
		want string
	}{
		{"HF 1.60 healthy", big.NewRat(1000, 1), StatusHealthy},
		{"HF exactly 1.15 healthy", big.NewRat(32000, 23), StatusHealthy},         // 1600/(32000/23)=1.15
		{"HF just under 1.15 margin call", big.NewRat(1400, 1), StatusMarginCall}, // 1600/1400=1.1428
		{"HF exactly 1.00 margin call", big.NewRat(1600, 1), StatusMarginCall},
		{"HF just under 1.00 liquidatable", big.NewRat(1700, 1), StatusLiquidatable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := e.Evaluate(context.Background(), acct, tc.debt)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != tc.want {
				t.Fatalf("HF=%s status=%s, want %s", r.HealthFactor, r.Status, tc.want)
			}
		})
	}
}

func TestNoDebtIsHealthyWithNAHealthFactor(t *testing.T) {
	e := testEngine(oneWETH(), big.NewRat(8, 10), big.NewRat(9, 10))
	r, err := e.Evaluate(context.Background(), acct, new(big.Rat))
	if err != nil {
		t.Fatal(err)
	}
	if r.HealthFactor != "n/a" || r.Status != StatusHealthy {
		t.Fatalf("HF=%s status=%s, want n/a healthy", r.HealthFactor, r.Status)
	}
}

func TestZeroBalancesSkipped(t *testing.T) {
	e := testEngine(StaticBalances{}, big.NewRat(8, 10), big.NewRat(9, 10))
	r, err := e.Evaluate(context.Background(), acct, big.NewRat(100, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Positions) != 0 || r.WeightedCollateralUSD != "0.00" {
		t.Fatalf("expected no positions, got %d weighted=%s", len(r.Positions), r.WeightedCollateralUSD)
	}
	// No collateral against debt -> HF 0 -> liquidatable.
	if r.Status != StatusLiquidatable {
		t.Fatalf("status=%s, want liquidatable", r.Status)
	}
}
