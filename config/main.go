package config

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fatih/color"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql" // Load MySQL for GORM
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"google.golang.org/api/option"
)

// Config is the main config structure and DB Model
type Config struct {
	gorm.Model
	Expires          string `gorm:"default:'48h'"`
	PrivateKey       []byte `gorm:"type:varbinary(4096);"`
	UserPrivateKey   []byte `gorm:"type:varbinary(4096);"`
	ServerPrivateKey []byte `gorm:"type:varbinary(4096);"`
	DefaultHosts     string `gorm:"type:MEDIUMTEXT;"`
}

// User is the model for users and their data
type User struct {
	gorm.Model
	Email           string `gorm:"type:varchar(255);"`
	AuthToken       string `gorm:"type:MEDIUMTEXT;"`
	CertExpires     time.Time
	Cert            []byte `gorm:"type:varbinary(4096);"`
	PrivateKey      []byte `gorm:"type:varbinary(4096);"`
	Authorized      bool   `gorm:"default:false"`
	AuthorizedHosts string `gorm:"type:MEDIUMTEXT;"`
	Admin           bool   `gorm:"default:false"`
	UnixUser        string `gorm:"type:varchar(255);"`
}

// Session is the model for a specific SSH sessions
type Session struct {
	gorm.Model
	Name     string `gorm:"type:MEDIUMTEXT;"`
	Time     time.Time
	Cast     string `gorm:"type:LONGTEXT;"`
	UserID   uint
	User     *User
	Host     string `gorm:"type:MEDIUMTEXT;"`
	Hostname string `gorm:"type:MEDIUMTEXT;"`
	Users    string `gorm:"type:LONGTEXT;"`
	Command  string `gorm:"type:MEDIUMTEXT;"`
}

// Env is our main context. A pointer of this is passed almost everywhere
type Env struct {
	GCE              bool
	ForceGeneration  bool
	PKPassphrase     string
	SSHServerClients *sync.Map
	SSHProxyClients  *sync.Map
	WebsocketClients *sync.Map
	DB               *gorm.DB
	Config           *Config
	LogsBucket       *storage.BucketHandle
	Vconfig          *viper.Viper
	Red              *ColorLog
	Green            *ColorLog
	Yellow           *ColorLog
	Blue             *ColorLog
	Magenta          *ColorLog
}

// WsClient is a struct that contains a websockets underlying data object
type WsClient struct {
	Client *websocket.Conn
}

// SSHServerClient is a struct containing the client (user's) SSH connection
type SSHServerClient struct {
	Client          *ssh.ServerConn
	RawProxyConn    net.Conn
	ProxyTo         string
	ProxyToHostname string
	Username        string
	Password        string
	PublicKey       ssh.PublicKey
	Agent           *agent.Agent
	User            *User
}

// SSHProxyClient is a struct containing the proxy (server's) SSH connection
type SSHProxyClient struct {
	Client           net.Conn
	SSHClient        *ssh.Client
	SSHServerClient  *SSHServerClient
	SSHShellSessions []*ConnChan
	SSHChans         []*ConnChan
	Mutex            *sync.Mutex
}

// ConnReq handles logged data from an SSH Request
type ConnReq struct {
	ReqType  string
	ReqData  []byte
	ReqReply bool
}

// ConnChan handles logged data from an SSH Channel
type ConnChan struct {
	ChannelType string
	ChannelData []byte
	Reqs        []*ConnReq
	ClientConn  *ssh.ServerConn
	ProxyConn   *ssh.Client
	ProxyChan   *ssh.Channel
	ClientChan  *ssh.Channel
	Closer      *AsciicastReadCloser
}

var configFile = "config.yml"

// Load initializes the Env pointer with data from the database and elsewhere
func Load(forceCerts bool) *Env {
	vconfig := viper.New()

	vconfig.SetConfigFile(configFile)
	vconfig.ReadInConfig()

	red := NewColorLog(color.New(color.FgRed))
	green := NewColorLog(color.New(color.FgGreen))
	yellow := NewColorLog(color.New(color.FgYellow))
	blue := NewColorLog(color.New(color.FgBlue))
	magenta := NewColorLog(color.New(color.FgMagenta))

	db, err := gorm.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8&parseTime=True&loc=Local", vconfig.GetString("dbinfo.user"), vconfig.GetString("dbinfo.pass"), vconfig.GetString("dbinfo.host"), vconfig.GetString("dbinfo.port"), vconfig.GetString("dbinfo.name")))
	if err != nil {
		red.Println("Error loading config:", err)
	}
	db.LogMode(vconfig.GetBool("debug"))

	db.AutoMigrate(&Config{}, &User{}, &Session{})

	var config Config

	db.First(&config)

	if config.Expires == "" {
		config.Expires = "48h"
	}

	ctx := context.Background()
	storageClient, err := storage.NewClient(ctx, option.WithCredentialsFile("credentials.json"))
	if err != nil {
		red.Println("Error initializing google cloud storage", err)
	}

	bucketName := vconfig.GetString("gce.bucket")
	logsBucket := storageClient.Bucket(bucketName)

	return &Env{
		GCE:              vconfig.GetBool("gce.enabled"),
		ForceGeneration:  forceCerts,
		PKPassphrase:     vconfig.GetString("pkpassphrase"),
		SSHServerClients: &sync.Map{},
		SSHProxyClients:  &sync.Map{},
		WebsocketClients: &sync.Map{},
		Config:           &config,
		DB:               db,
		LogsBucket:       logsBucket,
		Vconfig:          vconfig,
		Red:              red,
		Green:            green,
		Yellow:           yellow,
		Blue:             blue,
		Magenta:          magenta,
	}
}

// Save saves current Env data into the database and configs
func Save(env *Env) {
	env.DB.Save(env.Config)
	env.Vconfig.WriteConfigAs(configFile)
}
