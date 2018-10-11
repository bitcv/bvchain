package state

import (
	"bvchain/ec"
	"bvchain/tx"
	"bvchain/tx/op"

	"fmt"
	"math/big"
)

type OperationFunction func(o op.Operation, address ec.Address, sdb *StateDb) error

var _opFuncTab = map[op.Kind]OperationFunction{
	op.OP_Transfer: do_Transfer,
	op.OP_SetupWitness: do_SetupWitness,
	op.OP_VoteWitness: do_VoteWitness,
}

func (sdb *StateDb) ApplyTransaction(trx tx.Transaction) error {
	address := trx.SenderAddress()
	fun := _opFuncTab[trx.Operation.Kind()]
	if fun == nil {
		return fmt.Errorf("No function found for the operation")
	}
	err := fun(trx.Operation, address, sdb)
	return err
}

func do_Transfer(operation op.Operation, address ec.Address, sdb *StateDb) error {
	o := operation.(*op.TransferOperation)
	from := sdb.getAccountState(address)
	if from == nil {
		return fmt.Errorf("Can't get the _AccountState for address(%s)", address)
	}

	to := sdb.getAccountState(o.Recipient)
	if to == nil {
		to, _ = sdb.createAccountState(o.Recipient)
	}

	fee := big.NewInt(10000*10000) // TODO
	payout := new(big.Int).Add(o.Amount, fee)

	if from.Balance().Cmp(payout) < 0 {
		return fmt.Errorf("Not enough balance to transfer")
	}

	from.SubBalance(payout)
	to.AddBalance(o.Amount)
	return nil
}

func do_SetupWitness(operation op.Operation, address ec.Address, sdb *StateDb) error {
	o := operation.(*op.SetupWitnessOperation)
	from := sdb.getAccountState(address)
	if from == nil {
		return fmt.Errorf("Can't get the _AccountState for address(%s)", address)
	}

	fee := big.NewInt(10000*10000*1000) // TODO
	if from.Balance().Cmp(fee) < 0 {
		return fmt.Errorf("Not enough balance to become witness")
	}
	from.SubBalance(fee)

	voterId := int64(0)
	if from.account.Vid < 0 {
		voterId = from.account.Vid
	}

	var w *WitnessState
	if from.account.Vid == 0 {
		w = sdb.createWitnessState(address, voterId)
		from.SetVid(w.WitnessId)
	} else {
		// TODO
		w = sdb.GetWitnessState(from.account.Vid)
	}

	w.SetUrl(o.Url, sdb)
	// TODO
	return nil
}

func do_VoteWitness(operation op.Operation, address ec.Address, sdb *StateDb) error {
	o := operation.(*op.VoteWitnessOperation)
	from := sdb.getAccountState(address)
	if from == nil {
		return fmt.Errorf("Can't get the _AccountState for address(%s)", address)
	}

	if from.account.Vid > 0 {
		return fmt.Errorf("Witness can't vote")
	}

	fee := big.NewInt(10000*10000) // TODO
	if from.Balance().Cmp(fee) < 0 {
		return fmt.Errorf("Not enough balance to vote")
	}
	from.SubBalance(fee)

	var v *VoterState
	if from.account.Vid == 0 {
		v = sdb.createVoterState(address)
		from.SetVid(v.VoterId)
	} else {
		// TODO
		v = sdb.GetVoterState(from.account.Vid)
	}

	v.SetNumWitness(o.NumWitness, sdb)
	v.SetWitnessIds(o.WitnessIds, sdb)
	// TODO
	return nil
}

