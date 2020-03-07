/*
Copyright © 2020 Tino Rusch <tino.rusch@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

var (
	cfgFile        string
	grpcConnection *grpc.ClientConn
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "backbonectl",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	//	Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initLogging)
	cobra.OnInitialize(initConfig)
	cobra.OnInitialize(initConnection)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.backbonectl.yaml)")
	rootCmd.PersistentFlags().String("server", "localhost:3001", "server to connect to")
	rootCmd.PersistentFlags().String("ca", "", "ca certificate to use")
	rootCmd.PersistentFlags().Bool("disable-tls", false, "disable")
	rootCmd.PersistentFlags().String("log-level", "INFO", "log level")
	viper.SetEnvPrefix("PLATFORMCTL")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.BindPFlags(rootCmd.PersistentFlags())
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".backbonectl" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".backbonectl")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func initLogging() {
	lvl, err := logrus.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.SetLevel(lvl)
}

func initConnection() {
	var (
		opts = []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithBackoffMaxDelay(5 * time.Second),
		}
		addr   = viper.GetString("server")
		caFile = viper.GetString("ca")
		err    error
	)
	logrus.Debugf("try connecting server %s", addr)
	if viper.GetBool("disable-tls") {
		opts = append(opts, grpc.WithInsecure())
	} else {
		var creds credentials.TransportCredentials
		if caFile != "" {
			creds, err = credentials.NewClientTLSFromFile(caFile, "")
			if err != nil {
				logrus.Fatal(err)
			}
		} else {
			certs, err := x509.SystemCertPool()
			if err != nil {
				logrus.Fatal(err)
			}
			creds = credentials.NewTLS(&tls.Config{
				RootCAs: certs,
			})
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		logrus.Fatal(err)
	}
	grpcConnection = conn
	logrus.Debugf("successfully connected to grpc server at %v", addr)
}
