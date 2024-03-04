package w3backend

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math/big"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/nthpool/w3rpc/fetcher"
)

type BadgerKV struct {
	bdb *badger.DB
}

func NewBadgerKV() *BadgerKV {
	bdb, err := badger.Open(badger.DefaultOptions("").WithInMemory(true))
	if err != nil {
		panic(err)
	}
	return &BadgerKV{
		bdb: bdb,
	}
}

func mergeState(oldState fetcher.StateOverride, newState fetcher.State) fetcher.StateOverride {
	for k, v := range newState {
		account := &fetcher.OverrideAccount{}
		if acc, ok := oldState[k]; ok {
			account = &acc
		} else {
			stateDiff := make(map[common.Hash]common.Hash)
			account.StateDiff = stateDiff
		}
		account.Balance = v.Balance
		account.Code = v.Code
		account.Nonce = (*hexutil.Uint64)(v.Nonce)
		for kh, vh := range v.Storage {
			account.StateDiff[kh] = vh
		}
		oldState[k] = *account
	}
	return oldState
}

func mergeAccount(o, n *fetcher.Account) *fetcher.Account {
	if n.Balance != nil {
		o.Balance = n.Balance
	}
	if n.Code != nil {
		o.Code = n.Code
	}
	if n.Nonce != nil {
		o.Nonce = n.Nonce
	}
	for k, v := range n.Storage {
		o.Storage[k] = v
	}
	return o
}

func (db *BadgerKV) ProcessState(s fetcher.TraceBlock[fetcher.PrestateTrace]) error {
	err := db.bdb.Update(func(txn *badger.Txn) error {
		for _, tr := range s {
			for k, v := range tr.Result.Pre {
				oa, err := db.txGetAccount(k, txn)
				if err == nil {
					v = mergeAccount(oa, v)
				}
				db.txUpdateAccount(k, v, txn)
			}
			for k, v := range tr.Result.Post {
				oa, err := db.txGetAccount(k, txn)
				if err == nil {
					v = mergeAccount(oa, v)
				}
				db.txUpdateAccount(k, v, txn)
			}
		}
		return nil
	})
	return err
}

type accountMarshaling struct {
	Balance *big.Int
	Code    []byte
	Nonce   *uint64
	Storage map[common.Hash]common.Hash
}

func toAccountMarshaling(account *fetcher.Account) *accountMarshaling {
	a := &accountMarshaling{}
	if account.Balance != nil {
		a.Balance = account.Balance.ToInt()
	}
	if account.Code != nil {
		a.Code = *account.Code
	}
	a.Nonce = account.Nonce
	a.Storage = account.Storage
	if a.Storage == nil {
		a.Storage = make(map[common.Hash]common.Hash)
	}
	return a
}

func fromAccountMarshaling(account *accountMarshaling) *fetcher.Account {
	a := &fetcher.Account{}
	a.Balance = (*hexutil.Big)(account.Balance)
	if account.Code != nil {
		a.Code = (*hexutil.Bytes)(&account.Code)
	}
	a.Nonce = account.Nonce
	a.Storage = account.Storage
	return a
}

func encodeAccount(account *fetcher.Account) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(toAccountMarshaling(account))
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), err
}

func decodeAccount(b []byte) (*fetcher.Account, error) {
	var account accountMarshaling
	var buf bytes.Buffer
	buf.Write(b)
	dec := gob.NewDecoder(&buf)
	err := dec.Decode(&account)
	return fromAccountMarshaling(&account), err
}

// assume this is called within a db tx
func (db *BadgerKV) txGetAccount(addr common.Address, txn *badger.Txn) (account *fetcher.Account, err error) {
	item, err := txn.Get(addr.Bytes())
	if err != nil {
		return nil, err
	}
	err = item.Value(func(val []byte) error {
		a, err := decodeAccount(val)
		account = a
		return err
	})
	return
}

func (db *BadgerKV) GetAccount(addr common.Address) (account *fetcher.Account, err error) {
	err = db.bdb.View(func(txn *badger.Txn) error {
		acnt, err := db.txGetAccount(addr, txn)
		account = acnt
		return err
	})
	return
}

func (db *BadgerKV) txUpdateAccount(addr common.Address, account *fetcher.Account, txn *badger.Txn) (err error) {
	buf, err := encodeAccount(account)
	if err != nil {
		return err
	}
	err = txn.Set(addr.Bytes(), buf)
	return
}

func (db *BadgerKV) UpdateAccount(addr common.Address, account *fetcher.Account) (err error) {
	err = db.bdb.Update(func(txn *badger.Txn) error {
		return db.txUpdateAccount(addr, account, txn)
	})
	return
}

func (db *BadgerKV) GetStorage(addr common.Address, key []byte) (v common.Hash, err error) {
	account, err := db.GetAccount(addr)
	if err != nil {
		return
	}
	v, ok := account.Storage[common.BytesToHash(key)]
	if !ok {
		return common.Hash{}, fmt.Errorf("storage key not found")
	}
	return
}

func (db *BadgerKV) UpdateStorage(addr common.Address, key, value []byte) error {
	err := db.bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(addr.Bytes())
		if err != nil {
			return err
		}
		var account *fetcher.Account
		err = item.Value(func(val []byte) error {
			a, err := decodeAccount(val)
			account = a
			return err
		})
		if err != nil {
			return err
		}
		account.Storage[common.BytesToHash(key)] = common.BytesToHash(value)
		buf, err := encodeAccount(account)
		if err != nil {
			return err
		}
		txn.Set(addr.Bytes(), buf)
		return err
	})
	return err
}

func (db *BadgerKV) ContractCode(addr common.Address, codeHash common.Hash) ([]byte, error) {
	account, err := db.GetAccount(addr)
	if err != nil {
		return nil, err
	}
	if account.Code == nil {
		return nil, nil
	}
	return ([]byte)(*account.Code), nil
}

func (db *BadgerKV) UpdateContractCode(addr common.Address, codeHash common.Hash, code []byte) error {
	err := db.bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(addr.Bytes())
		if err != nil {
			return err
		}
		var account *fetcher.Account
		err = item.Value(func(val []byte) error {
			a, err := decodeAccount(val)
			account = a
			return err
		})
		if err != nil {
			return err
		}
		account.Code = (*hexutil.Bytes)(&code)
		buf, err := encodeAccount(account)
		if err != nil {
			return err
		}
		txn.Set(addr.Bytes(), buf)
		return err
	})
	return err
}
