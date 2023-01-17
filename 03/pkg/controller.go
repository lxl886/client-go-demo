package pkg

import (
	"context"
	core "k8s.io/api/core/v1"
	net "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	informer "k8s.io/client-go/informers/core/v1"
	netInformer "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	coreLister "k8s.io/client-go/listers/core/v1"
	v1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"reflect"
	"time"
)

const (
	workNum  = 5
	maxRetry = 10
)

type controller struct {
	client        kubernetes.Interface
	ingressLister v1.IngressLister
	serviceLister coreLister.ServiceLister
	queue         workqueue.RateLimitingInterface
}

func (c *controller) addService(obj interface{}) {
	c.enqueue(obj)
}

func (c *controller) updateService(oldObj interface{}, newObj interface{}) {
	// todo 比较annotation
	if reflect.DeepEqual(oldObj, newObj) {
		return
	}
	c.enqueue(newObj)
}

func (c *controller) deleteIngress(obj interface{}) {
	ingress := obj.(*net.Ingress)
	ownerReference := meta.GetControllerOf(ingress)
	if ownerReference == nil {
		return
	}
	if ownerReference.Kind != "Service" {
		return
	}

	c.queue.Add(ingress.Namespace + "/" + ingress.Name)
}

// 放入queue
func (c *controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
	}
	c.queue.Add(key)
}

func (c *controller) Run(stopCh <-chan struct{}) {

	// 创建worker
	for i := 0; i < workNum; i++ {
		go wait.Until(c.worker, time.Minute, stopCh)
	}
	<-stopCh
}

func (c *controller) worker() {
	for c.processNextItem() {

	}
}

// 从queue中获取，并执行对应的处理
func (c *controller) processNextItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(item)

	key := item.(string)

	err := c.syncService(key)
	if err != nil {
		c.handlerError(key, err)
	}
	return true
}

// 处理service
func (c *controller) syncService(key string) error {
	// 获取ns和name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	// 对是否删除判断
	service, err := c.serviceLister.Services(namespace).Get(name)
	if err != nil {
		return err
	}
	// 新增和更新
	_, ok := service.GetAnnotations()["ingress/http"]
	ingress, err := c.ingressLister.Ingresses(namespace).Get(name)
	// 对除了ingress不存在的情况特殊处理，其他错误返回
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if ok && errors.IsNotFound(err) {
		// 对service的annotation存在的，ingress不存在的进行创建
		ig := constructIngress(service)
		_, err := c.client.NetworkingV1().Ingresses(namespace).Create(context.TODO(), ig, meta.CreateOptions{})
		if err != nil {
			return err
		}
	} else if !ok && ingress != nil {
		// 对service的annotation不存在的，ingress存在的进行删除
		err := c.client.NetworkingV1().Ingresses(namespace).Delete(context.TODO(), name, meta.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func constructIngress(service *core.Service) *net.Ingress {
	ingress := net.Ingress{}

	ingress.ObjectMeta.OwnerReferences = []meta.OwnerReference{
		*meta.NewControllerRef(service, core.SchemeGroupVersion.WithKind("Service")),
	}

	ingress.Name = service.Name
	ingress.Namespace = service.Namespace
	pathType := net.PathTypePrefix
	icn := "nginx"
	ingress.Spec = net.IngressSpec{
		IngressClassName: &icn,
		Rules: []net.IngressRule{
			{
				Host: "client-go-demo.com",
				IngressRuleValue: net.IngressRuleValue{
					HTTP: &net.HTTPIngressRuleValue{
						Paths: []net.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: net.IngressBackend{
									Service: &net.IngressServiceBackend{
										Name: service.Name,
										Port: net.ServiceBackendPort{
											Number: 80,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return &ingress
}

func (c *controller) handlerError(key string, err error) {
	if c.queue.NumRequeues(key) <= maxRetry {
		c.queue.AddRateLimited(key)
		return
	}
	runtime.HandleError(err)
	c.queue.Forget(key)
}

func NewController(client kubernetes.Interface, serviceInformer informer.ServiceInformer, ingressInformer netInformer.IngressInformer) controller {
	// 创建controller
	c := controller{
		client:        client,
		ingressLister: ingressInformer.Lister(),
		serviceLister: serviceInformer.Lister(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ingressManager"),
	}

	// 添加事件处理方法
	// 为什么没有关注删除事件，还需要在syncService里面处理删除的逻辑？
	// 1. syncService里面的是不知道这个key是什么事件产生的，表述的时候跟事件联系起来是为了方便理解。
	// 而且，queue里面放什么样的信息都是可以的，只是推荐的是放key而已
	// 2. 自定义controller没有关注删除事件，但是我们的informer里仍然有删除事件相关的代码逻辑，
	// 比如：从indexer里面删除对应的对象信息，这也就是为什么在查询key对应的资源对象时，会对不存在（也就是删除）的情况进行处理了
	// 3. 为什么我们处理更新或者新增事件传递的key时，会存在找不到我们资源对象不存在的情况呢？
	// 因为，当我们还没进行处理这些事件时，可能又产生了删除事件
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addService,
		UpdateFunc: c.updateService,
	})

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteIngress,
	})

	return c
}
