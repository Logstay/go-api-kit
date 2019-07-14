package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/resty.v1"
)

type ConfigServer interface {
	Load(env EnvSet) error
}

type EnvSet func(key string, value interface{})

func Nop(key string, value interface{}) {}

type configServer struct {
	application       string
	profile           string
	server            string
	label             string
	token             string
	tokenLookupOrigin string
	tokenLookupValue  string
	vaultToken        string
}

type springCloudConfig struct {
	Name            string           `json:"name"`
	Profiles        []string         `json:"profiles"`
	Label           string           `json:"label"`
	Version         string           `json:"version"`
	PropertySources []propertySource `json:"propertySources"`
}

type propertySource struct {
	Index  int                    `json:"index"`
	Name   string                 `json:"name"`
	Source map[string]interface{} `json:"source"`
}

type Options struct {
	App               string
	Profile           string
	Server            string
	Label             string
	Token             string
	TokenLookupOrigin string
	TokenLookupValue  string
	VaultToken        string
}

type Option func(opt *Options)

func NewConfigServer(args ...Option) ConfigServer {
	options := Options{
		VaultToken:        "none",
		TokenLookupOrigin: "header",
		TokenLookupValue:  "apikey",
	}

	for _, o := range args {
		o(&options)
	}

	return &configServer{
		application:       options.App,
		profile:           options.Profile,
		server:            options.Server,
		label:             options.Label,
		token:             options.Token,
		tokenLookupOrigin: options.TokenLookupOrigin,
		tokenLookupValue:  options.TokenLookupValue,
		vaultToken:        options.VaultToken,
	}
}

func App(name string) Option {
	return func(opt *Options) {
		opt.App = name
	}
}

func Profile(name string) Option {
	return func(opt *Options) {
		opt.Profile = name
	}
}

func Label(name string) Option {
	return func(opt *Options) {
		opt.Label = name
	}
}

func Server(uri string) Option {
	return func(opt *Options) {
		opt.Server = uri
	}
}

func Token(token string) Option {
	return func(opt *Options) {
		opt.Token = token
	}
}

func KeyTokenLookupFromHeader(key string) Option {
	return func(opt *Options) {
		opt.TokenLookupOrigin = "header"
		opt.TokenLookupValue = key
	}
}

func KeyTokenLookupFromQuery(key string) Option {
	return func(opt *Options) {
		opt.TokenLookupOrigin = "query"
		opt.TokenLookupValue = key
	}
}

func VaultToken(token string) Option {
	return func(opt *Options) {
		opt.VaultToken = token
	}
}

// Load config from file to viper
func (s *configServer) Load(env EnvSet) error {
	url := fmt.Sprintf("%s/%s/%s/%s", s.server, s.application, s.profile, s.label)

	if s.tokenLookupOrigin == "query" {
		url = fmt.Sprintf("%s?apikey=%s", url, s.token)
	}

	logrus.Infof("Loading config from %s", url)

	body, err := s.fetch(url)
	if err != nil {
		return fmt.Errorf("Couldn't load configuration, cannot start. Terminating. Error:%s", err.Error())
	}

	cmd := Nop
	if env != nil {
		cmd = env
	}

	return s.parse(body, cmd)
}

func (s *configServer) fetch(url string) ([]byte, error) {
	client := resty.New()

	resp, err := client.
		SetDebug(false).
		SetDisableWarn(true).
		SetRetryCount(5).
		SetRetryWaitTime(5*time.Second).
		SetRetryMaxWaitTime(20*time.Second).
		SetHeader("vault_token", s.vaultToken).
		SetHeader(s.tokenLookupValue, s.token).
		R().
		Get(url)

	if err != nil {
		return nil, fmt.Errorf("Couldn't load configuration, cannot start. Terminating. Error: %s", err.Error())
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("Couldn't load configuration, parse error: %s", resp.String())
	}

	return resp.Body(), nil
}

func (s *configServer) parse(body []byte, env EnvSet) error {
	var cloudConfig springCloudConfig

	err := json.Unmarshal(body, &cloudConfig)
	if err != nil {
		return fmt.Errorf("Cannot parse configuration, message: %s", err.Error())
	}

	sort.SliceStable(cloudConfig.PropertySources, func(a, b int) bool {
		return cloudConfig.PropertySources[a].Index > cloudConfig.PropertySources[b].Index
	})

	for i := len(cloudConfig.PropertySources) - 1; i >= 0; i-- {
		props := cloudConfig.PropertySources[i]

		for key, value := range props.Source {
			env(key, value)
			logrus.Debugf("Loading config property %v => %v\n", key, value)
		}
	}

	return nil
}
