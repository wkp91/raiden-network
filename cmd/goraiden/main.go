package main

import (
	"os"

	"fmt"

	"strings"

	"io/ioutil"

	"encoding/hex"

	"path"

	"path/filepath"

	"time"

	"net"

	"github.com/SmartMeshFoundation/raiden-network"
	"github.com/SmartMeshFoundation/raiden-network/network"
	"github.com/SmartMeshFoundation/raiden-network/network/rpc"
	"github.com/SmartMeshFoundation/raiden-network/params"
	"github.com/SmartMeshFoundation/raiden-network/utils"
	"github.com/davecgh/go-spew/spew"
	ethutils "github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/kataras/go-fs"
	"github.com/slonzok/getpass"
	"gopkg.in/urfave/cli.v1"
)

func main() {

	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "address",
			Usage: "The ethereum address you would like raiden to use and for which a keystore file exists in your local system.",
		},
		ethutils.DirectoryFlag{
			Name:  "keystore-path",
			Usage: "If you have a non-standard path for the ethereum keystore directory provide it using this argument. ",
			Value: ethutils.DirectoryString{params.DefaultKeyStoreDir()},
		},
		cli.StringFlag{
			Name: "eth-rpc-endpoint",
			Usage: `"host:port" address of ethereum JSON-RPC server.\n'
	           'Also accepts a protocol prefix (ws:// or ipc channel) with optional port',`,
			Value: node.DefaultIPCEndpoint("geth"),
		},
		cli.StringFlag{
			Name:  "registry-contract-address",
			Usage: `hex encoded address of the registry contract.`,
			Value: params.ROPSTEN_REGISTRY_ADDRESS.String(),
		},
		cli.StringFlag{
			Name:  "discovery-contract-address",
			Usage: `hex encoded address of the discovery contract.`,
			Value: params.ROPSTEN_DISCOVERY_ADDRESS.String(),
		},
		cli.StringFlag{
			Name:  "listen-address",
			Usage: `"host:port" for the raiden service to listen on.`,
			Value: fmt.Sprintf("0.0.0.0:%d", params.INITIAL_PORT),
		},
		cli.StringFlag{
			Name: "rpccorsdomain",
			Usage: `Comma separated list of domains to accept cross origin requests.
				(localhost enabled by default)`,
			Value: "http://localhost:* /*",
		},
		cli.StringFlag{
			Name:  "logging",
			Usage: `ethereum.slogging config-string{trace,debug,info,warn,error,critical `,
			Value: "trace",
		},
		cli.StringFlag{
			Name:  "logfile",
			Usage: "file path for logging to file",
			Value: "",
		},
		cli.IntFlag{Name: "max-unresponsive-time",
			Usage: `Max time in seconds for which an address can send no packets and
	               still be considered healthy.`,
			Value: 120,
		},
		cli.IntFlag{Name: "send-ping-time",
			Usage: `Time in seconds after which if we have received no message from a
	               node we have a connection with, we are going to send a PING message`,
			Value: 60,
		},
		cli.BoolTFlag{Name: "rpc",
			Usage: `Start with or without the RPC server. Default is to start
	               the RPC server`,
		},
		cli.StringFlag{
			Name:  "api-address",
			Usage: `host:port" for the RPC server to listen on.`,
			Value: "0.0.0.0:5001",
		},
		ethutils.DirectoryFlag{
			Name:  "datadir",
			Usage: "Directory for storing raiden data.",
			Value: ethutils.DirectoryString{params.DefaultDataDir()},
		},
		cli.StringFlag{
			Name:  "password-file",
			Usage: "Text file containing password for provided account",
		},
	}
	app.Action = Main
	app.Name = "raiden"
	app.Version = "0.2"
	app.Run(os.Args)
}
func setupLog(ctx *cli.Context) {
	loglevel := strings.ToLower(ctx.String("logging"))
	writer := os.Stderr
	lvl := log.LvlTrace
	switch loglevel {
	case "trace":
		lvl = log.LvlTrace
	case "debug":
		lvl = log.LvlDebug
	case "info":
		lvl = log.LvlInfo
	case "warn":
		lvl = log.LvlWarn
	case "error":
		lvl = log.LvlError
	case "critical":
		lvl = log.LvlCrit
	}
	logfilename := ctx.String("logfile")
	if len(logfilename) > 0 {
		file, err := os.OpenFile(logfilename, os.O_WRONLY, os.ModePerm)
		if err != nil {
			fmt.Sprint("open logfile %s error:%s", logfilename, err)
			os.Exit(1)
		}
		writer = file
	}
	log.Root().SetHandler(log.LvlFilterHandler(lvl, log.StreamHandler(writer, log.TerminalFormat(true))))
}
func Main(ctx *cli.Context) error {
	fmt.Printf("Welcom to GoRaiden,version %s", ctx.App.Version)
	//promptAccount(utils.EmptyAddress, `D:\privnet\keystore\`, "")
	setupLog(ctx)
	/*
	 # TODO:
	       # - Ask for confirmation to quit if there are any locked transfers that did
	       # not timeout.
	*/
	host, port := network.SplitHostPort(ctx.String("listen-address"))
	pms, err := network.SocketFactory(host, port, ctx.String("nat"))
	log.Trace("pms=", utils.StringInterface1(pms))
	if err != nil {
		log.Error(fmt.Sprintf("start server on %s error:%s", ctx.String("listen-address"), err))
		os.Exit(1)
	}
	cfg := config(ctx, pms)
	spew.Dump("Config:", cfg)
	ethEndpoint := ctx.String("eth-rpc-endpoint")
	client, err := ethclient.Dial(ethEndpoint)
	if err != nil {
		log.Error("cannot connect to geth :%s", ethEndpoint)
		os.Exit(1)
	}
	bcs := rpc.NewBlockChainService(cfg.PrivateKey, cfg.RegistryAddress, client)
	log.Trace(fmt.Sprintf("bcs=%#v", bcs))
	//discovery := network.NewContractDiscovery(bcs.NodeAddress, bcs.Client, bcs.Auth)
	discovery := network.NewHttpDiscovery()
	//if err = discovery.Register(bcs.NodeAddress, host, port); err != nil {
	//	log.Error("discovery endpoint register error:", err)
	//	os.Exit(1)
	//}
	registry, _ := rpc.NewRegistry(bcs.RegistryAddress, bcs.Client)
	policy := network.NewTokenBucket(10, 1, time.Now)
	transport := network.NewUDPTransport(host, port, pms.Conn.(*net.UDPConn), nil, policy)
	raidenService := raiden_network.NewRaidenService(bcs, registry, cfg.PrivateKey, transport, discovery, cfg)
	raidenService.Start()
	return nil
}
func promptAccount(adviceAddress common.Address, keystorePath, passwordfile string) (addr common.Address, keybin []byte) {
	am := raiden_network.NewAccountManager(keystorePath)
	if len(am.Accounts) == 0 {
		log.Error(fmt.Sprintf("No Ethereum accounts found in the directory %s", keystorePath))
		os.Exit(1)
	}
	if !am.AddressInKeyStore(adviceAddress) {
		if adviceAddress != utils.EmptyAddress {
			log.Error(fmt.Sprintf("account %s could not be found on the sytstem. aborting...", adviceAddress))
			os.Exit(1)
		}
		shouldPromt := true
		fmt.Println("The following accounts were found in your machine:")
		for i := 0; i < len(am.Accounts); i++ {
			fmt.Printf("%3d -  %s\n", i, am.Accounts[i].Address.String())
		}
		fmt.Println("")
		for shouldPromt {
			fmt.Printf("Select one of them by index to continue:\n")
			idx := -1
			fmt.Scanf("%d", &idx)
			if idx >= 0 && idx < len(am.Accounts) {
				shouldPromt = false
				addr = am.Accounts[idx].Address
			} else {
				fmt.Printf("Error: Provided index %d is out of bounds", idx)
			}
		}
	} else {
		addr = adviceAddress
	}
	var password string
	var err error
	if len(passwordfile) > 0 {
		data, err := ioutil.ReadFile(passwordfile)
		if err != nil {
			log.Error(fmt.Sprintf("password_file error:%s", err))
			os.Exit(1)
		}
		password = string(data)
		keybin, err = am.GetPrivateKey(addr, password)
		if err != nil {
			log.Error(fmt.Sprintf("Incorrect password for %S in file. Aborting ...", addr))
			os.Exit(1)
		}
	} else {
		for i := 0; i < 3; i++ {
			//retries three times
			password = getpass.Prompt("Enter the password to unlock:")
			keybin, err = am.GetPrivateKey(addr, password)
			if err != nil && i == 3 {
				log.Error(fmt.Sprintf("Exhausted passphrase unlock attempts for %s. Aborting ...", addr))
				os.Exit(1)
			}
			if err != nil {
				log.Error(fmt.Sprintf("password incorrect\n Please try again or kill the process to quit.\nUsually Ctrl-c."))
				continue
			}
			break
		}
	}
	return
}
func config(ctx *cli.Context, pms *network.PortMappedSocket) *params.Config {
	var err error
	config := params.DefaultConfig
	listenhost, listenport := network.SplitHostPort(ctx.String("listen-address"))
	apihost, apiport := network.SplitHostPort(ctx.String("api-address"))
	config.Host = listenhost
	config.Port = listenport
	config.UseConsole = ctx.Bool("console")
	config.UseRpc = ctx.Bool("rpc")
	config.ApiHost = apihost
	config.ApiPort = apiport
	config.ExternIp = pms.ExternalIp
	config.ExternPort = pms.ExternalPort
	max_unresponsive_time := ctx.Int64("max-unresponsive-time")
	config.Protocol.NatKeepAliveTimeout = max_unresponsive_time / params.DEFAULT_NAT_KEEPALIVE_RETRIES
	address := common.HexToAddress(ctx.String("address"))
	address, privkeyBin := promptAccount(address, ctx.String("keystore-path"), ctx.String("password-file"))
	config.PrivateKeyHex = hex.EncodeToString(privkeyBin)
	config.PrivateKey, err = crypto.ToECDSA(privkeyBin)
	config.MyAddress = address
	if err != nil {
		log.Error("privkey error:", err)
		os.Exit(1)
	}
	registAddrStr := ctx.String("registry-contract-address")
	if len(registAddrStr) > 0 {
		config.RegistryAddress = common.HexToAddress(registAddrStr)
	}
	discoverAddr := ctx.String("discovery-contract-address")
	if len(discoverAddr) > 0 {
		config.DiscoveryAddress = common.HexToAddress(discoverAddr)
	}
	dataDir := ctx.String("datadir")
	if len(dataDir) == 0 {
		dataDir = path.Join(fs.GetHomePath(), ".goraiden")
	}
	config.DataDir = dataDir
	if !utils.Exists(config.DataDir) {
		err = os.MkdirAll(config.DataDir, os.ModePerm)
		if err != nil {
			log.Error(fmt.Sprintf("Datadir:%s doesn't exist and cannot create %v", config.DataDir, err))
			os.Exit(1)
		}
	}
	userDbPath := hex.EncodeToString(config.MyAddress[:])
	userDbPath = userDbPath[:8]
	userDbPath = filepath.Join(config.DataDir, userDbPath)
	if !utils.Exists(userDbPath) {
		err = os.MkdirAll(userDbPath, os.ModePerm)
		if err != nil {
			log.Error(fmt.Sprintf("Datadir:%s doesn't exist and cannot create %v", userDbPath, err))
			os.Exit(1)
		}
	}
	databasePath := filepath.Join(userDbPath, "log.db")
	config.DataBasePath = databasePath
	return &config
}
