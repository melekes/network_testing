package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/tendermint/go-common"
	cfg "github.com/tendermint/go-config"
	"github.com/tendermint/go-p2p"

	tmcfg "github.com/tendermint/tendermint/config/tendermint"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

func main() {

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println(`Tendermint

Commands:
    node            Run the tendermint node
    show_validator  Show this node's validator info
    gen_validator   Generate new validator keypair
    probe_upnp      Test UPnP functionality
    version         Show version info
`)
		return
	}

	if len(args) < 2 {
		fmt.Println("mintbench expects another arg (number of txs)")
	}

	cmd, nTxsString := args[0], args[1]
	nTxs, err := strconv.Atoi(nTxsString)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Get configuration
	config := tmcfg.GetConfig("")
	parseFlags(config, args[1:]) // Command line overrides
	cfg.ApplyConfig(config)      // Notify modules of new config

	switch cmd {
	case "node":
		RunNode(nTxs)
	case "replay":
		if len(args) > 1 && args[1] == "console" {
			node.RunReplayConsole()
		} else {
			node.RunReplay()
		}
	case "init":
		init_files()
	case "show_validator":
		show_validator()
	case "gen_validator":
		gen_validator()
	case "probe_upnp":
		probe_upnp()
	case "unsafe_reset_all":
		reset_all()
	case "unsafe_reset_priv_validator":
		reset_priv_validator()
	case "version":
		fmt.Println(version.Version)
	default:
		Exit(Fmt("Unknown command %v\n", args[0]))
	}
}

// Users wishing to use an external signer for their validators
// should fork tendermint/tendermint and implement RunNode to
// load their custom priv validator and call NewNode(privVal)
func RunNode(nTxs int) {
	// Wait until the genesis doc becomes available
	genDocFile := config.GetString("genesis_file")
	if !FileExists(genDocFile) {
		log.Notice(Fmt("Waiting for genesis file %v...", genDocFile))
		for {
			time.Sleep(time.Second)
			if !FileExists(genDocFile) {
				continue
			}
			jsonBlob, err := ioutil.ReadFile(genDocFile)
			if err != nil {
				Exit(Fmt("Couldn't read GenesisDoc file: %v", err))
			}
			genDoc := types.GenesisDocFromJSON(jsonBlob)
			if genDoc.ChainID == "" {
				PanicSanity(Fmt("Genesis doc %v must include non-empty chain_id", genDocFile))
			}
			config.Set("chain_id", genDoc.ChainID)
			config.Set("genesis_doc", genDoc)
		}
	}

	// Get PrivValidator
	privValidatorFile := config.GetString("priv_validator_file")
	privValidator := types.LoadOrGenPrivValidator(privValidatorFile)

	// Create & start node
	n := node.NewNode(privValidator)
	l := p2p.NewDefaultListener("tcp", config.GetString("node_laddr"), config.GetBool("skip_upnp"))
	n.AddListener(l)
	err := n.Start()
	if err != nil {
		Exit(Fmt("Failed to start node: %v", err))
	}

	// If seedNode is provided by config, dial out.
	if config.GetString("seeds") != "" {
		seeds := strings.Split(config.GetString("seeds"), ",")
		n.Switch().DialSeeds(seeds)
	}

	// Run the RPC server.
	if config.GetString("rpc_laddr") != "" {
		_, err := n.StartRPC()
		if err != nil {
			PanicCrisis(err)
		}
	}

	// preload some txs into mempool
	log.Notice(Fmt("Preloading %d txs into mempool and disabling the reactor", nTxs))
	start := time.Now()
	for i := 0; i < nTxs; i++ {
		if i%1000 == 0 {
			log.Notice(Fmt("Loaded %d txs into mempool", i))
		}
		tx := make([]byte, 250)
		binary.PutUvarint(tx[242:], uint64(i))
		if _, err := rand.Read(tx[:128]); err != nil {
			Exit(err.Error())
		}
		if err := n.MempoolReactor().BroadcastTx(tx, nil); err != nil {
			Exit(err.Error())
		}
	}
	log.Notice("Done generating txs", "time", time.Since(start))

	// Sleep forever and then...
	TrapSignal(func() {
		n.Stop()
	})
}
