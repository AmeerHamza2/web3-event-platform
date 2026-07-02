package margin

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

// fakeCaller returns a fixed, pre-encoded eth_call result.
type fakeCaller struct{ out []byte }

func (f fakeCaller) CallContract(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	return f.out, nil
}

func TestBalanceReaderDecodesUint256(t *testing.T) {
	parsed, _ := abi.JSON(strings.NewReader(erc20BalanceOfABI))
	want := new(big.Int).Mul(big.NewInt(42), pow10(18))
	out, err := parsed.Methods["balanceOf"].Outputs.Pack(want)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewChainBalanceReader(fakeCaller{out: out})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.BalanceOf(context.Background(), weth, acct)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(want) != 0 {
		t.Fatalf("balance = %s, want %s", got, want)
	}
}

func TestChainlinkOracleDecodesAnswer(t *testing.T) {
	parsed, _ := abi.JSON(strings.NewReader(aggregatorABI))
	answer := new(big.Int).Mul(big.NewInt(2000), pow10(8)) // $2000, 8 decimals
	out, err := parsed.Methods["latestRoundData"].Outputs.Pack(
		big.NewInt(1), answer, big.NewInt(0), big.NewInt(0), big.NewInt(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewChainlinkOracle(fakeCaller{out: out}, 8)
	if err != nil {
		t.Fatal(err)
	}
	p, err := o.Price(context.Background(), wethFeed)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rat().FloatString(2) != "2000.00" {
		t.Fatalf("price = %s, want 2000.00", p.Rat().FloatString(2))
	}
}
