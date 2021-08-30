# nsqd-prometheus-exporter [![Build Status](https://travis-ci.org/adamweiner/nsqd-prometheus-exporter.svg?branch=master)](https://travis-ci.org/adamweiner/nsqd-prometheus-exporter)

Scrapes nsqd stats and serves them up as Prometheus metrics.

If a previously detected topic or channel no longer exists in a new scrape, the exporter will rebuild all metrics to remove any label values associated with the old topic or channel.


### Get started
#### binaries
```
make build
./nsqd-prometheus-exporter -n http://nsqd1:4151 -n http://nsqd2:4151 -p 30000
```

#### docker up
```
docker run -p 30000:2112 jinmz:nsqd-prometheus-exporter:v0.1 -n http://nsqd1:4151 -n http://nsqd2:4151 -p 2112
```

#### k8s 
```
k apply -f deployments/deploy.yaml
```

### request 
```
curl http://<host:port>/metrics
```


### refer 
* [https://github.com/adamweiner/nsqd-prometheus-exporter](https://github.com/adamweiner/nsqd-prometheus-exporter)