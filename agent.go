package main

import (
	"os"
	"path"
	"runtime"
	"time"

	. "github.com/tutumcloud/tutum-agent/agent"
	"github.com/tutumcloud/tutum-agent/utils"
)

func init() {
	runtime.GOMAXPROCS(4)
}

func main() {
	dockerBinPath := path.Join(DockerDir, DockerBinaryName)
	dockerNewBinPath := path.Join(DockerDir, DockerNewBinaryName)
	dockerNewBinSigPath := path.Join(DockerDir, DockerNewBinarySigName)
	configFilePath := path.Join(TutumHome, ConfigFileName)
	keyFilePath := path.Join(TutumHome, KeyFileName)
	certFilePath := path.Join(TutumHome, CertFileName)
	caFilePath := path.Join(TutumHome, CAFileName)

	ParseFlag()
	SetLogger(path.Join(LogDir, TutumLogFileName))

	Logger.Println("Preparing directories and files...")
	PrepareFiles(configFilePath, dockerBinPath, keyFilePath, certFilePath)

	SetConfigFile(configFilePath)

	url := utils.JoinURL(Conf.TutumHost, RegEndpoint)
	if Conf.TutumUUID == "" {
		Logger.Printf("Removing all existing cert and key files", url)
		os.RemoveAll(keyFilePath)
		os.RemoveAll(certFilePath)
		os.RemoveAll(caFilePath)

		if !*FlagStandalone {
			Logger.Printf("Registering in Tutum via POST: %s ...\n", url)
			PostToTutum(url, caFilePath, configFilePath)
		}
	}

	Logger.Println("Checking if TLS certificate exists...")
	if *FlagStandalone {
		commonName := Conf.CertCommonName
		if commonName == "" {
			commonName = "*"
		}
		CreateCerts(keyFilePath, certFilePath, commonName)
	} else {
		CreateCerts(keyFilePath, certFilePath, Conf.CertCommonName)
	}

	if !*FlagStandalone {
		Logger.Printf("Registering in Tutum via PATCH: %s ...\n", url+Conf.TutumUUID)
		err := PatchToTutum(url, caFilePath, certFilePath, configFilePath)
		if err != nil {
			Logger.Printf("TutumUUID (%s) is invalid, trying to allocate a new one ...\n", Conf.TutumUUID)
			Logger.Printf("Clearing invalid TutumUUID:%s ...\n", Conf.TutumUUID)
			Conf.TutumUUID = ""
			Logger.Print("Saving configuation to file ...")
			SaveConf(configFilePath, Conf)

			Logger.Printf("Removing all existing cert and key files", url)
			os.RemoveAll(keyFilePath)
			os.RemoveAll(certFilePath)
			os.RemoveAll(caFilePath)

			Logger.Printf("Registering in Tutum via POST: %s ...\n", url)
			PostToTutum(url, caFilePath, configFilePath)

			Logger.Println("Checking if TLS certificate exists...")
			CreateCerts(keyFilePath, certFilePath, Conf.CertCommonName)

			Logger.Printf("Registering in Tutum via PATCH: %s ...\n", url+Conf.TutumUUID)
			PatchToTutum(url, caFilePath, certFilePath, configFilePath)
		}
	}
	Logger.Println("Check if docker binary exists...")
	DownloadDocker(DockerBinaryURL, dockerBinPath)

	Logger.Println("Setting system signals...")
	HandleSig()

	Logger.Println("Starting docker daemon...")
	StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath)

	Logger.Println("Docker server started. Entering maintenance loop")
	for {
		time.Sleep(HeartBeatInterval * time.Second)
		UpdateDocker(dockerBinPath, dockerNewBinPath, dockerNewBinSigPath, keyFilePath, certFilePath, caFilePath)

		// try to restart docker daemon if it dies somehow
		if DockerProcess == nil {
			time.Sleep(HeartBeatInterval * time.Second)
			if DockerProcess == nil && ScheduleToTerminateDocker == false {
				Logger.Println("Respawning docker daemon")
				StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath)
			}
		}
	}
}

func PrepareFiles(configFilePath, dockerBinPath, keyFilePath, certFilePath string) {
	Logger.Println("Creating all necessary folders...")
	_ = os.MkdirAll(TutumHome, 0755)
	_ = os.MkdirAll(DockerDir, 0755)
	_ = os.MkdirAll(LogDir, 0755)

	Logger.Println("Checking if config file exists...")
	if utils.FileExist(configFilePath) {
		Logger.Println("Config file exist, skipping")
	} else {
		Logger.Println("Creating a new config file")
		LoadDefaultConf()
		if err := SaveConf(configFilePath, Conf); err != nil {
			Logger.Fatalln(err)
		}
	}

	Logger.Println("Loading Configuration file...")
	conf, err := LoadConf(configFilePath)
	if err != nil {
		Logger.Fatalln("Failed to load configuration file:", err)
	} else {
		Conf = *conf
	}

	if *FlagDockerHost != "" {
		Logger.Printf("Override 'DockerHost' from command line flag: %s\n", *FlagDockerHost)
		Conf.DockerHost = *FlagDockerHost
	}
	if *FlagTutumHost != "" {
		Logger.Printf("Override 'TutumHost' from command line flag: %s\n", *FlagTutumHost)
		Conf.TutumHost = *FlagTutumHost
	}
	if *FlagTutumToken != "" {
		Logger.Printf("Override 'TutumToken' from command line flag: %s\n", *FlagTutumToken)
		Conf.TutumToken = *FlagTutumToken
	}
	if *FlagTutumUUID != "" {
		Logger.Printf("Override 'TutumUUID' from command line flag: %s\n", *FlagTutumUUID)
		Conf.TutumUUID = *FlagTutumUUID
	}
}
