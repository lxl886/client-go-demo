# 选择了golang:1.18-alpine3.16作为编译的基础镜像，相比于golang:1.18, 主要是因为它体积更小。
FROM golang:1.18-alpine3.16 AS builder
WORKDIR /build
#接着是创建了一个app-runner的用户, -D表示无密码。
#此用户的信息是是需要拷到final中，作为应用程序的启动用户。这是为了避免使用container中的默认用户root，那可是有安全漏洞的，详细解释，
#可以参考这篇medium上的文章Processes In Containers Should Not Run As Root
RUN adduser -u 10001 -D app-runner

#是配置了国内的代理，安装依赖包了。这里用go mod download的好处是下次构建镜像文件时，当go.mod和go.sum没有改变时，它是有缓存的，可以避免重复下载依赖包，加快构建。
#ENV GOPROXY https://goproxy.cn
ENV GO111MODULE=on \
    GOPROXY=https://goproxy.cn,direct \
    GIN_MODE=release

COPY go.mod .
COPY go.sum .
RUN go mod download

#builder的最后，就是把当前目录的文件拷过去，编译代码了。
COPY . .
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -a -o ingress-manager .

#final选择了alpine:3.16,一方面是体积小；另一方面也是和构建镜像的alpine版本保持一致。
FROM alpine:3.16 AS final

WORKDIR /app

ENV GO_ENV=test

COPY --from=builder /build/ /app/
#COPY --from=builder /build/config /app/config
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

#没有它，container启动时就是用root用户启动了!!! 如果被攻击了，那黑客可是就有root权限了
USER app-runner
ENTRYPOINT ["/app/ingress-manager"]