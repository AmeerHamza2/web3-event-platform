package chainmonitor

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Minimal ERC-20 ABI: just the Transfer event, which is all the indexer decodes.
const erc20TransferABI = `[{
	"anonymous": false,
	"name": "Transfer",
	"type": "event",
	"inputs": [
		{"indexed": true,  "name": "from",  "type": "address"},
		{"indexed": true,  "name": "to",    "type": "address"},
		{"indexed": false, "name": "value", "type": "uint256"}
	]
}]`

// Transfer is a decoded ERC-20 Transfer log.
type Transfer struct {
	Token    common.Address
	From     common.Address
	To       common.Address
	Value    *big.Int
	TxHash   common.Hash
	LogIndex uint
}

// transferDecoder decodes ERC-20 Transfer logs. The parsed ABI is reused across
// calls; TopicID is the event signature hash used to filter logs.
type transferDecoder struct {
	abi     abi.ABI
	TopicID common.Hash
}

func newTransferDecoder() (*transferDecoder, error) {
	parsed, err := abi.JSON(strings.NewReader(erc20TransferABI))
	if err != nil {
		return nil, fmt.Errorf("parse erc20 abi: %w", err)
	}
	return &transferDecoder{abi: parsed, TopicID: parsed.Events["Transfer"].ID}, nil
}

// decode turns a raw log into a Transfer. Indexed fields (from, to) come from
// the topics; the non-indexed value is unpacked from the data. A log that isn't
// a well-formed Transfer is rejected.
func (d *transferDecoder) decode(log types.Log) (Transfer, bool) {
	if len(log.Topics) != 3 || log.Topics[0] != d.TopicID {
		return Transfer{}, false
	}
	var ev struct{ Value *big.Int }
	if err := d.abi.UnpackIntoInterface(&ev, "Transfer", log.Data); err != nil {
		return Transfer{}, false
	}
	return Transfer{
		Token:    log.Address,
		From:     common.HexToAddress(log.Topics[1].Hex()),
		To:       common.HexToAddress(log.Topics[2].Hex()),
		Value:    ev.Value,
		TxHash:   log.TxHash,
		LogIndex: log.Index,
	}, true
}
