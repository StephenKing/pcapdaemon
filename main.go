package main

import (
	"flag"
	"fmt"
	"log"
	"log/syslog"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/google/gopacket/pcap"

	// setup aws config
	"github.com/aws/aws-sdk-go/aws"
)

var cshostPtr *string
var cstokenPtr *string
var csschemePtr *string
var upPtr *bool
var wLocal *bool
var panicPtr *bool
var destdir *string
var hostname string
var maxpackets *int
var config tomlConfig
var awsconfig *aws.Config
var ifmap = map[string]pcap.Interface{}
var almap = make(map[string][]string)

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	// return fmt.Print(time.Now().UTC().Format("2006-01-02T15:04:05.999Z") + " [DEBUG] " + string(bytes))
	return fmt.Print(string(bytes))
}

func updateInterfaceMap() {

	x, ierr := pcap.FindAllDevs()
	if ierr != nil {
		fmt.Printf("Error loading interfaces: %s", ierr)
		log.Printf("Error loading interfaces: %s", ierr)
	}

	for _, v := range x {
		fmt.Println("Found interface " + v.Name + " description: " + v.Description)
		log.Println("Found interface " + v.Name + " description: " + v.Description)
		ifmap[v.Name] = v
	}
}

func validateOptions(c tomlConfig) {

	if !c.Aws.Upload && !c.Cs.Upload && !c.Gen.Writelocal {
		log.Printf("Error.  You must enable at least one of the following: Cloudshark, S3 or Writelocal\n")
		os.Exit(1)
	}

	if !c.AwsSqs.Listen && !c.R.Listen && !c.K.Listen {
		log.Printf("Error.  You must enable at least one listener: Kafka, Redis or Amazon SQS\n")
		os.Exit(1)
	}

	// S3 configs
	if c.Aws.Upload {
		if c.Aws.Bucket == nil {
			log.Fatal("You must supply a default bucket to use S3 ")
		}

		if c.Aws.ACL == nil {
			// default acl to private
			*c.Aws.ACL = "private"
		}

		if c.Aws.Encryption == nil {
			// set encryption default
			*c.Aws.Encryption = false
		}

		dest := new(bool)
		*dest = true
		ll := new(aws.LogLevelType)
		*ll = 1

		awsconfig = &aws.Config{
			Region:           c.Aws.Region,
			Endpoint:         c.Aws.Endpoint,
			S3ForcePathStyle: dest, // <-- without these lines. All will fail! fork you aws!
			LogLevel:         ll,
		}
	}

	// SQS Configs
	if c.AwsSqs.Listen {
		if c.AwsSqs.Region == nil {
			log.Fatal("You must supply a Region to use SQS ")
		}
		if c.AwsSqs.URL == nil {
			log.Fatal("You must supply a Queue URL to use SQS ")
		}

		if c.AwsSqs.Waitseconds == nil {
			*c.AwsSqs.Waitseconds = 20
		}

		if c.AwsSqs.Chunksize == nil {
			*c.AwsSqs.Waitseconds = 10
		}

	}

	// Kafka configs
	if c.K.Listen {
		if len(c.K.Server) < 1 {
			log.Fatal("You must supply at least 1 Kafka node")
		}
		if c.K.Topic == "" {
			log.Fatal("You must supply a Kafka topic to subscribe to")
		}
	}

	// Redis configs
	if c.R.Listen {
		if c.R.Host == "" {
			log.Fatal("You must supply a redis host")
		}
		if c.R.Channel == "" {
			log.Fatal("You must supply a redis channel to subscribe to")
		}
		if c.R.Port == 0 {
			c.R.Port = 6379
		}
	}

	// Cloudshark configs
	if c.Cs.Upload {
		if c.Cs.Host == "" {
			log.Fatal("If using Cloudshark, you must supply a host")
		}
		if c.Cs.Token == "" {
			log.Fatal("If using Cloudshark, you must supply an API token")
		}
		if c.Cs.Scheme == "" {
			c.Cs.Scheme = "https"
		}
		if c.Cs.Port == 0 {
			c.Cs.Port = 443
		}
	} else {
		fmt.Println("No cloudshark settings")
		log.Println("No cloudshark settings")
	}

	// General configs
	if c.Gen.Maxpackets == 0 {
		c.Gen.Maxpackets = 10000
	}
	if c.Gen.Snap == 0 {
		c.Gen.Snap = 512
	}

	if c.Gen.Writelocal {
		if _, err := os.Stat(c.Gen.Localdir); os.IsNotExist(err) {
			log.Fatal("localdir: " + c.Gen.Localdir + " does not exist")
		}
	}

	if c.Gen.Deftimeout == 0 {
		c.Gen.Deftimeout = 60
	}

	if c.Gen.Maxtimeout == 0 {
		c.Gen.Maxtimeout = 3600 * time.Second
	}

	if c.Gen.Maxduration == 0 {
		c.Gen.Maxduration = 3600 * time.Second
	}

	if c.Gen.Maxbytes == 0 {
		// set to 100 megabytes if max not set
		c.Gen.Maxbytes = 100000000
	}

	for _, v := range c.Ifmap {
		if v.Name == "" {
			log.Fatal("Interface definition missing name property")
		}
		if _, ok := ifmap[v.Name]; !ok {
			log.Fatal("Interface " + v.Name + " does not exist on host")
		}
		if len(v.Alias) == 0 {
			fmt.Println("Warning:  Interface [" + v.Name + "] has no aliases")
			log.Println("Warning:  Interface [" + v.Name + "] has no aliases")
		} else {
			for _, av := range v.Alias {
				almap[av] = append(almap[av], v.Name)
			}
		}
	}

	// validate syslog
	if c.Log.Priority == 0 {
		c.Log.Priority = 85
	}

	if c.Log.Tag == "" {
		c.Log.Tag = "pcapdaemon"
	}

}

func main() {

	cshostPtr = flag.String("cshost", "", "cloudshark host")
	cstokenPtr = flag.String("cstoken", "", "cloudshark api token")
	csschemePtr = flag.String("csscheme", "", "cloudshark scheme http|https")
	csportPtr := flag.Int("csport", 0, "cloudshark port")
	cstimeoutPtr := flag.Int("cstimeout", 0, "cloudshark timeout")
	configfile := flag.String("config", "", "/path/to/configfile")
	redisnode := flag.String("redisnode", "", "Hostname|IP of redis server.  Default localhost")
	redisport := flag.Int("redisport", 0, "Port of redis server. Default 6379")
	redischannel := flag.String("redischannel", "", "Redis channel to subscribe to.  Default capture")
	maxpackets = flag.Int("maxpackets", 0, "Maximum number of packets per capture.  Default 50000")

	upPtr = flag.Bool("upload", false, "Upload pcap")
	panicPtr = flag.Bool("panic", false, "Enable panics on layer parsing")

	// flags for writing locally
	wLocal = flag.Bool("writelocal", false, "Write files locally.  Must set destdir")
	destdir = flag.String("destdir", "", "Destination directory locally for pcap files.  Requires -writelocal")

	// parse the flags
	flag.Parse()

	if *configfile != "" {
		if _, err := os.Stat(*configfile); os.IsNotExist(err) {
			log.Fatal(*configfile + " does not exist")
		} else {
			if _, err := toml.DecodeFile(*configfile, &config); err != nil {
				log.Fatal(err)
			}
		}
	}

	log.SetFlags(0)
	logwriter, e := syslog.New(syslog.Priority(config.Log.Priority), config.Log.Tag)
	if e == nil {
		log.SetOutput(logwriter)
	}

	// create interface map
	updateInterfaceMap()

	// Overrides.  Command line parameters, if set, override the config file
	if *redisnode != "" {
		config.R.Host = *redisnode
	}
	if *redisport != 0 {
		config.R.Port = *redisport
	}
	if *redischannel != "" {
		config.R.Channel = *redischannel
	}

	if *cshostPtr != "" {
		config.Cs.Host = *cshostPtr
	}
	if *cstokenPtr != "" {
		config.Cs.Token = *cstokenPtr
	}
	if *csschemePtr != "" {
		config.Cs.Scheme = *csschemePtr
	}
	if *csportPtr != 0 {
		config.Cs.Port = *csportPtr
	}
	if *cstimeoutPtr != 0 {
		config.Cs.Timeout = *cstimeoutPtr
	}
	if *upPtr {
		config.Cs.Upload = true
	}

	if *maxpackets != 0 {
		config.Gen.Maxpackets = *maxpackets
	}

	if *wLocal {
		config.Gen.Writelocal = true
		if *destdir != "" {
			config.Gen.Localdir = *destdir
		}
	}

	// Run the validator AFTER defaults and config have been processed
	validateOptions(config)

	hostname, _ = os.Hostname()
	// Channel for thread sync
	done := make(chan bool)

	if config.R.Listen {
		go func() {
			log.Println("Starting Redis Thread")
			subToRedis(config.R.Host, config.R.Port, config.R.Channel, config.R.Auth)
			done <- true
		}()
	}

	if config.K.Listen {
		go func() {
			log.Println("Starting Kafka Thread")
			subToKafka(config.K.Server, config.K.Topic)
			done <- true
		}()
	}

	if config.AwsSqs.Listen {
		go func() {
			log.Println("Starting SQS Thread")
			subToSqs()
			done <- true
		}()
	}

	// This little channel read prevents main() from ending
	<-done
	log.Println("Exiting")
}
