package staking

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"strings"

	"github.com/umbracle/go-web3/abi"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/contracts/staking"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/state"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	MinValidatorCount = uint64(1)
	MaxValidatorCount = common.MaxSafeJSInt
)

// getAddressMapping returns the key for the SC storage mapping (address => something)
//
// More information:
// https://docs.soliditylang.org/en/latest/internals/layout_in_storage.html
func getAddressMapping(address types.Address, slot int64) []byte {
	bigSlot := big.NewInt(slot)

	finalSlice := append(
		common.PadLeftOrTrim(address.Bytes(), 32),
		common.PadLeftOrTrim(bigSlot.Bytes(), 32)...,
	)
	keccakValue := keccak.Keccak256(nil, finalSlice)

	return keccakValue
}

// getIndexWithOffset is a helper method for adding an offset to the already found keccak hash
func getIndexWithOffset(keccakHash []byte, offset int64) []byte {
	bigOffset := big.NewInt(offset)
	bigKeccak := big.NewInt(0).SetBytes(keccakHash)

	bigKeccak.Add(bigKeccak, bigOffset)

	return bigKeccak.Bytes()
}

// getStorageIndexes is a helper function for getting the correct indexes
// of the storage slots which need to be modified during bootstrap.
//
// It is SC dependant, and based on the SC located at:
// https://github.com/0xPolygon/staking-contracts/
func getStorageIndexes(address types.Address, index int64) *StorageIndexes {
	storageIndexes := StorageIndexes{}

	// Get the indexes for the mappings
	// The index for the mapping is retrieved with:
	// keccak(address . slot)
	// . stands for concatenation (basically appending the bytes)
	storageIndexes.AddressToIsValidatorIndex = getAddressMapping(address, addressToIsValidatorSlot)
	storageIndexes.AddressToStakedAmountIndex = getAddressMapping(address, addressToStakedAmountSlot)
	storageIndexes.AddressToValidatorIndexIndex = getAddressMapping(address, addressToValidatorIndexSlot)

	// Get the indexes for _validators, _stakedAmount
	// Index for regular types is calculated as just the regular slot
	storageIndexes.StakedAmountIndex = big.NewInt(stakedAmountSlot).Bytes()

	// Index for array types is calculated as keccak(slot) + index
	// The slot for the dynamic arrays that's put in the keccak needs to be in hex form (padded 64 chars)
	storageIndexes.ValidatorsIndex = getIndexWithOffset(
		keccak.Keccak256(nil, common.PadLeftOrTrim(big.NewInt(validatorsSlot).Bytes(), 32)),
		index,
	)

	// For any dynamic array in Solidity, the size of the actual array should be
	// located on slot x
	storageIndexes.ValidatorsArraySizeIndex = []byte{byte(validatorsSlot)}

	return &storageIndexes
}

// PredeployParams contains the values used to predeploy the PoS staking contract
type PredeployParams struct {
	MinValidatorCount uint64
	MaxValidatorCount uint64
}

// StorageIndexes is a wrapper for different storage indexes that
// need to be modified
type StorageIndexes struct {
	ValidatorsIndex              []byte // []address
	ValidatorsArraySizeIndex     []byte // []address size
	AddressToIsValidatorIndex    []byte // mapping(address => bool)
	AddressToStakedAmountIndex   []byte // mapping(address => uint256)
	AddressToValidatorIndexIndex []byte // mapping(address => uint256)
	StakedAmountIndex            []byte // uint256
}

// Slot definitions for SC storage
var (
	validatorsSlot              = int64(0) // Slot 0
	addressToIsValidatorSlot    = int64(1) // Slot 1
	addressToStakedAmountSlot   = int64(2) // Slot 2
	addressToValidatorIndexSlot = int64(3) // Slot 3
	stakedAmountSlot            = int64(4) // Slot 4
	minNumValidatorSlot         = int64(5) // Slot 5
	maxNumValidatorSlot         = int64(6) // Slot 6
)

const (
	DefaultStakedBalance = "0x8AC7230489E80000" // 10 ETH
	//nolint: lll
	StakingSCBytecode = "0x6080604052600436106100f75760003560e01c80637dceceb81161008a578063e387a7ed11610059578063e387a7ed14610381578063e804fbf6146103ac578063f90ecacc146103d7578063facd743b1461041457610165565b80637dceceb8146102c3578063af6da36e14610300578063c795c0771461032b578063ca1e78191461035657610165565b8063373d6132116100c6578063373d6132146102385780633a4b66f114610263578063714ff4251461026d5780637a6eea371461029857610165565b806302b751991461016a578063065ae171146101a75780632367f6b5146101e45780632def66201461022157610165565b366101655761011b3373ffffffffffffffffffffffffffffffffffffffff16610451565b1561015b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610152906111a0565b60405180910390fd5b610163610464565b005b600080fd5b34801561017657600080fd5b50610191600480360381019061018c9190610f1e565b61053b565b60405161019e91906111fb565b60405180910390f35b3480156101b357600080fd5b506101ce60048036038101906101c99190610f1e565b610553565b6040516101db9190611125565b60405180910390f35b3480156101f057600080fd5b5061020b60048036038101906102069190610f1e565b610573565b60405161021891906111fb565b60405180910390f35b34801561022d57600080fd5b506102366105bc565b005b34801561024457600080fd5b5061024d6106a7565b60405161025a91906111fb565b60405180910390f35b61026b6106b1565b005b34801561027957600080fd5b5061028261071a565b60405161028f91906111fb565b60405180910390f35b3480156102a457600080fd5b506102ad610724565b6040516102ba91906111e0565b60405180910390f35b3480156102cf57600080fd5b506102ea60048036038101906102e59190610f1e565b610730565b6040516102f791906111fb565b60405180910390f35b34801561030c57600080fd5b50610315610748565b60405161032291906111fb565b60405180910390f35b34801561033757600080fd5b5061034061074e565b60405161034d91906111fb565b60405180910390f35b34801561036257600080fd5b5061036b610754565b6040516103789190611103565b60405180910390f35b34801561038d57600080fd5b506103966107e2565b6040516103a391906111fb565b60405180910390f35b3480156103b857600080fd5b506103c16107e8565b6040516103ce91906111fb565b60405180910390f35b3480156103e357600080fd5b506103fe60048036038101906103f99190610f4b565b6107f2565b60405161040b91906110e8565b60405180910390f35b34801561042057600080fd5b5061043b60048036038101906104369190610f1e565b610831565b6040516104489190611125565b60405180910390f35b600080823b905060008111915050919050565b34600460008282546104769190611260565b9250508190555034600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546104cc9190611260565b925050819055506104dc33610887565b156104eb576104ea336108ff565b5b3373ffffffffffffffffffffffffffffffffffffffff167f9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d3460405161053191906111fb565b60405180910390a2565b60036020528060005260406000206000915090505481565b60016020528060005260406000206000915054906101000a900460ff1681565b6000600260008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020549050919050565b6105db3373ffffffffffffffffffffffffffffffffffffffff16610451565b1561061b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610612906111a0565b60405180910390fd5b6000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541161069d576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161069490611140565b60405180910390fd5b6106a5610a4e565b565b6000600454905090565b6106d03373ffffffffffffffffffffffffffffffffffffffff16610451565b15610710576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610707906111a0565b60405180910390fd5b610718610464565b565b6000600554905090565b670de0b6b3a764000081565b60026020528060005260406000206000915090505481565b60065481565b60055481565b606060008054806020026020016040519081016040528092919081815260200182805480156107d857602002820191906000526020600020905b8160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001906001019080831161078e575b5050505050905090565b60045481565b6000600654905090565b6000818154811061080257600080fd5b906000526020600020016000915054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b6000600160008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900460ff169050919050565b600061089282610ba0565b1580156108f85750670de0b6b3a76400006fffffffffffffffffffffffffffffffff16600260008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410155b9050919050565b60065460008054905010610948576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161093f90611160565b60405180910390fd5b60018060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548160ff021916908315150217905550600080549050600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055506000819080600181540180825580915050600190039060005260206000200160009091909190916101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050565b6000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490506000600260003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508060046000828254610ae991906112b6565b92505081905550610af933610ba0565b15610b0857610b0733610bf6565b5b3373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f19350505050158015610b4e573d6000803e3d6000fd5b503373ffffffffffffffffffffffffffffffffffffffff167f0f5bb82176feb1b5e747e28471aa92156a04d9f3ab9f45f28e2d704232b93f7582604051610b9591906111fb565b60405180910390a250565b6000600160008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900460ff169050919050565b60055460008054905011610c3f576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610c36906111c0565b60405180910390fd5b600080549050600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410610cc5576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610cbc90611180565b60405180910390fd5b6000600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054905060006001600080549050610d1d91906112b6565b9050808214610e0b576000808281548110610d3b57610d3a6113ac565b5b9060005260206000200160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690508060008481548110610d7d57610d7c6113ac565b5b9060005260206000200160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555082600360008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002081905550505b6000600160008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548160ff0219169083151502179055506000600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055506000805480610eba57610eb961137d565b5b6001900381819060005260206000200160006101000a81549073ffffffffffffffffffffffffffffffffffffffff02191690559055505050565b600081359050610f03816114f9565b92915050565b600081359050610f1881611510565b92915050565b600060208284031215610f3457610f336113db565b5b6000610f4284828501610ef4565b91505092915050565b600060208284031215610f6157610f606113db565b5b6000610f6f84828501610f09565b91505092915050565b6000610f848383610f90565b60208301905092915050565b610f99816112ea565b82525050565b610fa8816112ea565b82525050565b6000610fb982611226565b610fc3818561123e565b9350610fce83611216565b8060005b83811015610fff578151610fe68882610f78565b9750610ff183611231565b925050600181019050610fd2565b5085935050505092915050565b611015816112fc565b82525050565b6000611028601d8361124f565b9150611033826113e0565b602082019050919050565b600061104b60278361124f565b915061105682611409565b604082019050919050565b600061106e60128361124f565b915061107982611458565b602082019050919050565b6000611091601a8361124f565b915061109c82611481565b602082019050919050565b60006110b460408361124f565b91506110bf826114aa565b604082019050919050565b6110d381611308565b82525050565b6110e281611344565b82525050565b60006020820190506110fd6000830184610f9f565b92915050565b6000602082019050818103600083015261111d8184610fae565b905092915050565b600060208201905061113a600083018461100c565b92915050565b600060208201905081810360008301526111598161101b565b9050919050565b600060208201905081810360008301526111798161103e565b9050919050565b6000602082019050818103600083015261119981611061565b9050919050565b600060208201905081810360008301526111b981611084565b9050919050565b600060208201905081810360008301526111d9816110a7565b9050919050565b60006020820190506111f560008301846110ca565b92915050565b600060208201905061121060008301846110d9565b92915050565b6000819050602082019050919050565b600081519050919050565b6000602082019050919050565b600082825260208201905092915050565b600082825260208201905092915050565b600061126b82611344565b915061127683611344565b9250827fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff038211156112ab576112aa61134e565b5b828201905092915050565b60006112c182611344565b91506112cc83611344565b9250828210156112df576112de61134e565b5b828203905092915050565b60006112f582611324565b9050919050565b60008115159050919050565b60006fffffffffffffffffffffffffffffffff82169050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000819050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b600080fd5b7f4f6e6c79207374616b65722063616e2063616c6c2066756e6374696f6e000000600082015250565b7f56616c696461746f72207365742068617320726561636865642066756c6c206360008201527f6170616369747900000000000000000000000000000000000000000000000000602082015250565b7f696e646578206f7574206f662072616e67650000000000000000000000000000600082015250565b7f4f6e6c7920454f412063616e2063616c6c2066756e6374696f6e000000000000600082015250565b7f56616c696461746f72732063616e2774206265206c657373207468616e20746860008201527f65206d696e696d756d2072657175697265642076616c696461746f72206e756d602082015250565b611502816112ea565b811461150d57600080fd5b50565b61151981611344565b811461152457600080fd5b5056fea26469706673582212208a8aa21d6df01384c9fc6d39a32e52ef1c0d18fd3bf9e2fca6ae1cae3d41268864736f6c63430008070033"
)

type ContractArtifact struct {
	ABI      string
	Bytecode string
}

type contractArtifact struct {
	ABI              []byte
	Bytecode         []byte
	DeployedBytecode []byte
}

func (c *contractArtifact) loadFromFile(filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return err
	}

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}

	var fileJSON map[string]interface{}
	if err := json.Unmarshal(bytes, &fileJSON); err != nil {
		return err
	}

	/*	parse abi */
	if err := c.setABI(fileJSON); err != nil {
		return err
	}

	/*	parse bytecode */
	if err := c.setBytecode(fileJSON); err != nil {
		return err
	}

	/*	parse deployed bytecode */
	if err := c.setDeployedBytecode(fileJSON); err != nil {
		return err
	}

	return nil
}

func (c *contractArtifact) setABI(jsonMap map[string]interface{}) error {
	rawABI, ok := jsonMap["contractABI"]
	if !ok {
		panic("bad")
	}

	contractABI, err := json.Marshal(rawABI)
	if err != nil {
		return err
	}

	c.ABI = contractABI

	return nil
}

func (c *contractArtifact) setBytecode(jsonMap map[string]interface{}) error {
	rawBytecode, ok := jsonMap["bytecode"].(string)
	if !ok {
		panic("bad")
	}

	bytecode, err := hex.DecodeString(strings.TrimPrefix(rawBytecode, "0x"))
	if err != nil {
		return err
	}

	c.Bytecode = bytecode

	return nil
}

func (c *contractArtifact) setDeployedBytecode(jsonMap map[string]interface{}) error {
	rawDeployedBytecode, ok := jsonMap["deployedBytecode"].(string)
	if !ok {
		panic("bad ")
	}

	deployedBytecode, err := hex.DecodeString(strings.TrimPrefix(rawDeployedBytecode, "0x"))
	if err != nil {
		return err
	}

	c.DeployedBytecode = deployedBytecode

	return nil
}

func (c *contractArtifact) encodeCustomConstructor(params ...interface{}) []byte {
	//	generate bytecode with custom constructor
	contractABI, err := abi.NewABI(string(c.ABI))
	if err != nil {
		return nil
	}

	//	#2: verify contract satisfies required interface
	//	TODO: maybe this should/must be done in generateContractArtifact

	constructor, err := abi.Encode(
		params,
		contractABI.Constructor.Inputs)
	if err != nil {
		return nil
	}

	finalBytecode := append(c.Bytecode, constructor...)

	return finalBytecode
}

func generateContractArtifact(filepath string) (*contractArtifact, error) {
	artifact := new(contractArtifact)
	if err := artifact.loadFromFile(filepath); err != nil {
		return nil, err
	}

	return artifact, nil
}

//	TODO: move this out to a separate helper package in end phase
func GenerateGenesisAccountFromFile(
	filepath string,
	constructorParams []interface{},
) (*chain.GenesisAccount, error) {
	//	#1: generate artifact from json file
	artifact, err := generateContractArtifact(filepath)
	if err != nil {
		return nil, err
	}

	// 	#2: encode custom constructor values to generate bytecode
	customBytecode := artifact.encodeCustomConstructor(constructorParams)

	//	TODO (milos): where does config come from ?
	config := chain.ForksInTime{
		Homestead:      true,
		Byzantium:      true,
		Constantinople: true,
		Petersburg:     true,
		Istanbul:       true,
		EIP150:         true,
		EIP158:         true,
		EIP155:         true,
	}

	//	#3: generate genesis account based on contract bytecode
	contractAccount, err := state.GenerateContractAccount(
		config,
		itrie.NewState(itrie.NewMemoryStorage()),
		staking.AddrStakingContract,
		customBytecode,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to generate contract account - err: %w", err)
	}

	return contractAccount, nil
}

// PredeployStakingSC is a helper method for setting up the staking smart contract account,
// using the passed in validators as pre-staked validators
func PredeployStakingSC(
	validators []types.Address,
	params PredeployParams,
) (*chain.GenesisAccount, error) {
	// Set the code for the staking smart contract
	// Code retrieved from https://github.com/0xPolygon/staking-contracts
	scHex, _ := hex.DecodeHex(StakingSCBytecode)
	stakingAccount := &chain.GenesisAccount{
		Code: scHex,
	}

	// Parse the default staked balance value into *big.Int
	val := DefaultStakedBalance
	bigDefaultStakedBalance, err := types.ParseUint256orHex(&val)

	if err != nil {
		return nil, fmt.Errorf("unable to generate DefaultStatkedBalance, %w", err)
	}

	// Generate the empty account storage map
	storageMap := make(map[types.Hash]types.Hash)
	bigTrueValue := big.NewInt(1)
	stakedAmount := big.NewInt(0)
	bigMinNumValidators := big.NewInt(int64(params.MinValidatorCount))
	bigMaxNumValidators := big.NewInt(int64(params.MaxValidatorCount))

	for indx, validator := range validators {
		// Update the total staked amount
		stakedAmount.Add(stakedAmount, bigDefaultStakedBalance)

		// Get the storage indexes
		storageIndexes := getStorageIndexes(validator, int64(indx))

		// Set the value for the validators array
		storageMap[types.BytesToHash(storageIndexes.ValidatorsIndex)] =
			types.BytesToHash(
				validator.Bytes(),
			)

		// Set the value for the address -> validator array index mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToIsValidatorIndex)] =
			types.BytesToHash(bigTrueValue.Bytes())

		// Set the value for the address -> staked amount mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToStakedAmountIndex)] =
			types.StringToHash(hex.EncodeBig(bigDefaultStakedBalance))

		// Set the value for the address -> validator index mapping
		storageMap[types.BytesToHash(storageIndexes.AddressToValidatorIndexIndex)] =
			types.StringToHash(hex.EncodeUint64(uint64(indx)))

		// Set the value for the total staked amount
		storageMap[types.BytesToHash(storageIndexes.StakedAmountIndex)] =
			types.BytesToHash(stakedAmount.Bytes())

		// Set the value for the size of the validators array
		storageMap[types.BytesToHash(storageIndexes.ValidatorsArraySizeIndex)] =
			types.StringToHash(hex.EncodeUint64(uint64(indx + 1)))
	}

	// Set the value for the minimum number of validators
	storageMap[types.BytesToHash(big.NewInt(minNumValidatorSlot).Bytes())] =
		types.BytesToHash(bigMinNumValidators.Bytes())

	// Set the value for the maximum number of validators
	storageMap[types.BytesToHash(big.NewInt(maxNumValidatorSlot).Bytes())] =
		types.BytesToHash(bigMaxNumValidators.Bytes())

	// Save the storage map
	stakingAccount.Storage = storageMap

	// Set the Staking SC balance to numValidators * defaultStakedBalance
	stakingAccount.Balance = stakedAmount

	return stakingAccount, nil
}
