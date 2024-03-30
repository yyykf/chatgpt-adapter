package pkg

import (
	"bytes"
	"github.com/spf13/viper"
	"os"
)

var (
	Config *viper.Viper
)

func LoadConfig(configPath string) (*viper.Viper, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	vip := viper.New()
	vip.SetConfigType("yaml")
	if err = vip.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	return vip, nil
}

func Init(configPath string) {
	config, err := LoadConfig(configPath)
	if err != nil {
		panic(err)
	}
	Config = config
}
