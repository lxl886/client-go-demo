package main

import (
	"fmt"
	"github.com/lxl886/client-go-demo/03/pkg"
	"github.com/spf13/viper"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
)

func main() {
	// config
	configEnv := os.Getenv("GO_ENV")
	fmt.Println("当前环境变量：" + configEnv)
	configFile := "./config/config-dev.yaml" // './config/confg-test.yaml'
	switch configEnv {
	case "dev":
		configFile = "./config/config-dev.yaml"
	case "test":
		configFile = "./config/config-test.yaml"
	case "nj":
		configFile = "./config/config-nj.yaml"
	}
	//使用 viper
	v := viper.New()
	v.SetConfigFile(configFile)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		panic(fmt.Errorf("read config failed: %s \n", err))
	}
	k8sConfig := v.GetString("k8sConfig")
	config, err := clientcmd.BuildConfigFromFlags("", k8sConfig)
	if err != nil {
		panic(fmt.Errorf("config error"))
	}
	// client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Errorf("client error"))
	}
	// informer
	factory := informers.NewSharedInformerFactory(client, 0)
	serviceInformer := factory.Core().V1().Services()
	ingressInformer := factory.Networking().V1().Ingresses()
	// add event
	controller := pkg.NewController(client, serviceInformer, ingressInformer)
	// start
	stopCh := make(chan struct{})
	factory.Start(stopCh)
	controller.Run(stopCh)
}
