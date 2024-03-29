# tdarr-exporter
- [tdarr-exporter](#tdarr-exporter)
  - [Background](#background)
  - [Usage](#usage)
  - [Configuration](#configuration)
  - [Dashboard](#dashboard)

## Background
`tdarr-exporter` is a Prometheus collector for [Tdarr](https://github.com/HaveAGitGat/Tdarr) and provides the following as Prometheus metrics:

1. General instance and library statistics (as shown in Tdarr's statistics page).
2. Current queues and counts.
3. Progress updates for running transcode and health check jobs.

Some samples below from Grafana, check `examples/` for more samples and a complete view of metrics.
<img src="./examples/images/demo_3.png" alt="overview" width="1050"/>
<img src="./examples/images/demo_1.png" alt="overview" width="1050"/>

Progress updates are provided for following:
1. CPU/GPU Flow plugin jobs
2. CPU/GPU Classic plugin jobs
3. CPU/GPU health check jobs

Inspired by exportarr and qbittorrent-exporter projects. I wanted to have everything in Grafana so I don't have to check everywhere.

## Usage
`tdarr-exporter` can be run from in the following ways:

1. Docker container
2. Executable binary
3. Helm chart (K8)

### Binary
Each tagged release will include executable binaries under the `assets` section of the release notes. This can be downloaded and run directly, see [configuration](#configuration) section for more details on run options.

`./tdarr-exporter -url=example.com`

A variety of platforms (`darwin`, `linux`, `freebsd`, `openbsd`) are supported and include `386`, `amd64`, and `arm64` variants.

### Docker
Docker images are provided for `linux/amd64` and `linux/arm64` variants only at the moment.

| tag | description |
| --- | ----------- |
| `latest` | Latest stable release and is updated with each new release. |
| `X.X.X`  | Semantic versioned releases are also provided if preferred for stability or other reasons. |
| `main` | This tag reflects the `main` branch of this repository and may not be stable |

To run this image, the `URL` should be provided, and more options can be supplied if needed. See [configuration](#configuration) section for more details.

`docker run -e TDARR_URL=example.com -p 9090:9090 homeylab/tdarr-exporter:latest`

### Helm
`tdarr-exporter` can be deployed to Kubernetes using the provided Helm chart. The chart is available in a separate [repository](https://github.com/homeylab/helm-charts/tree/main/charts/tdarr-exporter).

## Configuration
`tdarr-exporter` accepts the following variables for configuration via the cli or environment variables.

Example
```bash
$ ./tdarr-exporter -h
  -log_level string
        log level to use, see link for possible values: https://pkg.go.dev/github.com/rs/zerolog#Level (default "info")
  -prometheus_path string
        path to use for prometheus exporter (default "/metrics")
  -prometheus_port string
        port for prometheus exporter (default "9090")
  -url string
        valid url for tdarr instance, ex: https://tdarr.somedomain.com
  -verify_ssl
        verify ssl certificates from tdarr (default true)
```

| Property          |  Environment Variable | Default    | Description |
| ----------------- | --------------------- | ---------- | ----------- |
| `url`             | `TDARR_URL`           | `NONE`     | This is a required property and must be provided. If no protocol is provided (`http/https`), defaults to using `https`. Examples: `tdarr.example.com`, `http://tdarr.example.com`. |
| `log_level`        | `LOG_LEVEL`           | `info`     | Log level to use: `debug`, `info`, `warn`, `error`. |
| `verify_ssl`      | `VERIFY_SSL`          | `true`     | Whether or not to verify ssl certificates. |
| `prometheus_port` | `PROMETHEUS_PORT`     | `9090`     | Which port for server to use to serve. metrics |
| `prometheus_path` | `PROMETHEUS_PATH`     | `/metrics` | Which path to serve metrics on. |

## Dashboard
Dashboard example can be found on Grafana's portal [here](https://grafana.com/grafana/dashboards/20388).
- Copy the ID `20388` and then import it in Grafana.

Dashboard example is also provided in the `examples/dashboard.json` file in case the dashboard from [Grafana](https://grafana.com/grafana/dashboards/20388) is not available.
- In Grafana, add a new dashboard and then copy and paste the `dashboard.json` file contents.