package zksync2

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/zksync-sdk/zksync2-go/contracts/ERC20"
	"github.com/zksync-sdk/zksync2-go/contracts/L1ERC20Bridge"
	"github.com/zksync-sdk/zksync2-go/contracts/L1EthBridge"
	"math/big"
	"strings"
)

type Wallet struct {
	es EthSigner
	zp Provider

	bcs      *BridgeContracts
	erc20abi abi.ABI
}

func NewWallet(es EthSigner, zp Provider) (*Wallet, error) {
	erc20abi, err := abi.JSON(strings.NewReader(ERC20.ERC20ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to load erc20abi: %w", err)
	}
	return &Wallet{
		es:       es,
		zp:       zp,
		erc20abi: erc20abi,
	}, nil
}

func (w *Wallet) CreateEthereumProvider(rpcClient *rpc.Client) (*DefaultEthProvider, error) {
	ethClient := ethclient.NewClient(rpcClient)
	chainId, err := ethClient.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain Id: %w", err)
	}
	auth, err := w.newTransactorWithSigner(w.es, chainId)
	if err != nil {
		return nil, fmt.Errorf("failed to init TransactOpts: %w", err)
	}
	bcs, err := w.getBridgeContracts()
	if err != nil {
		return nil, fmt.Errorf("failed to getBridgeContracts: %w", err)
	}
	l1EthBridge, err := L1EthBridge.NewL1EthBridge(bcs.L1EthDefaultBridge, ethClient)
	if err != nil {
		return nil, fmt.Errorf("failed to load L1EthBridge: %w", err)
	}
	l1ERC20Bridge, err := L1ERC20Bridge.NewL1ERC20Bridge(bcs.L1Erc20DefaultBridge, ethClient)
	if err != nil {
		return nil, fmt.Errorf("failed to load L1ERC20Bridge: %w", err)
	}
	return NewDefaultEthProvider(rpcClient, auth, l1EthBridge, l1ERC20Bridge), nil
}

func (w *Wallet) GetBalance() (*big.Int, error) {
	return w.zp.GetBalance(w.es.GetAddress(), BlockNumberCommitted, CreateETH())
}

func (w *Wallet) GetNonce() (*big.Int, error) {
	return w.zp.GetTransactionCount(w.es.GetAddress(), BlockNumberCommitted)
}

func (w *Wallet) Transfer(to common.Address, amount *big.Int, token *Token, nonce *big.Int, feeToken *Token) (*types.Transaction, error) {
	var err error
	if token == nil {
		token = CreateETH()
	}
	if feeToken == nil {
		feeToken = CreateETH()
	}
	if nonce == nil {
		nonce, err = w.GetNonce()
		if err != nil {
			return nil, fmt.Errorf("failed to get nonce: %w", err)
		}
	}
	var data hexutil.Bytes
	if !token.IsETH() {
		data, err = w.erc20abi.Pack("transfer", to, amount)
		if err != nil {
			return nil, fmt.Errorf("failed to pack transfer function: %w", err)
		}
		to = token.L2Address
		amount = big.NewInt(0)
	}
	tx := CreateFunctionCallTransaction(
		w.es.GetAddress(), to, big.NewInt(0), big.NewInt(0), amount, feeToken.L2Address, data)
	return w.estimateAndSend(tx, nonce)
}

func (w *Wallet) estimateAndSend(tx *Transaction, nonce *big.Int) (*types.Transaction, error) {
	gas, err := w.zp.EstimateGas(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to EstimateGas: %w", err)
	}
	fmt.Println(gas)
	chainId := w.es.GetDomain().ChainId
	fmt.Println(chainId)

	//prepared := NewTransaction712(nonce, tx.To, tx.Value.Int, gas, big.NewInt(0), tx.Data, chainId, tx.Eip712Meta)

	return nil, nil
}

func (w *Wallet) getBridgeContracts() (*BridgeContracts, error) {
	if w.bcs != nil {
		return w.bcs, nil
	}
	var err error
	w.bcs, err = w.zp.ZksGetBridgeContracts()
	if err != nil {
		return nil, err
	}
	return w.bcs, nil
}

func (w *Wallet) newTransactorWithSigner(ethSigner EthSigner, chainID *big.Int) (*bind.TransactOpts, error) {
	if chainID == nil {
		return nil, bind.ErrNoChainID
	}
	keyAddr := ethSigner.GetAddress()
	signer := types.LatestSignerForChainID(chainID)
	return &bind.TransactOpts{
		From: keyAddr,
		Signer: func(address common.Address, tx *types.Transaction) (*types.Transaction, error) {
			if address != keyAddr {
				return nil, bind.ErrNotAuthorized
			}
			signature, err := ethSigner.SignHash(signer.Hash(tx).Bytes())
			if err != nil {
				return nil, err
			}
			return tx.WithSignature(signer, signature)
		},
	}, nil
}