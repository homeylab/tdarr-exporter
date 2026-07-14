# Changelog

## [3.1.0](https://github.com/homeylab/tdarr-exporter/compare/v3.0.0...v3.1.0) (2026-07-13)


### Features

* add instance_name flag / INSTANCE_NAME env var ([c4d015b](https://github.com/homeylab/tdarr-exporter/commit/c4d015bec0d71e51cb868ccdfa63e3ca4af4088c))
* add listen_address flag / LISTEN_ADDRESS env var ([55c3c50](https://github.com/homeylab/tdarr-exporter/commit/55c3c50a0bdd0a02f1ad090690342e521a9c31fe))
* add response status to request log; harden Recovery middleware ([4e67ede](https://github.com/homeylab/tdarr-exporter/commit/4e67edebdcd5fd8248229ba1192f4d4866e26a43))
* add response status to request log; harden Recovery middleware ([1e0af7d](https://github.com/homeylab/tdarr-exporter/commit/1e0af7d2c54dbb534d4beddbb633252851061312))
* configurable listen_address and instance_name ([8862843](https://github.com/homeylab/tdarr-exporter/commit/88628437722c4276efd86d697a100ee834684664))
* label tdarr_exporter_build_info with tdarr_instance ([967d02d](https://github.com/homeylab/tdarr-exporter/commit/967d02d0aa2f244d25343453ddef86891dc388ad))
* remove tdarr_scrape_duration_seconds metric ([b09301d](https://github.com/homeylab/tdarr-exporter/commit/b09301d97f656df1c79e7aaba4c1d1189ac9bf79))
* remove tdarr_scrape_duration_seconds metric ([20a5764](https://github.com/homeylab/tdarr-exporter/commit/20a5764ded269dc26b3b2a16601990d3b3b8a7a8))


### Bug Fixes

* abort retry backoff when request context is cancelled ([d29c25f](https://github.com/homeylab/tdarr-exporter/commit/d29c25fcc272f929422d2e01b8dc18cce9f1837a))
* abort retry backoff when request context is cancelled ([47b7972](https://github.com/homeylab/tdarr-exporter/commit/47b797262b474466e88691271879f375279b5efd))
* bracket IPv6 listen_address with net.JoinHostPort ([82a4b88](https://github.com/homeylab/tdarr-exporter/commit/82a4b887f8e3dd3e184d3f9262663a82b1b73647))
* bracket IPv6 listen_address with net.JoinHostPort ([9b68497](https://github.com/homeylab/tdarr-exporter/commit/9b684972e9d288c403517d10614b61fcce66a59a))
* **deps:** update minor & patch updates ([fd7ecdf](https://github.com/homeylab/tdarr-exporter/commit/fd7ecdff4ea3835bd65f90e05570b502b8e4188c))
* **deps:** update minor & patch updates ([85c5959](https://github.com/homeylab/tdarr-exporter/commit/85c59597dc6d0783aeb0a648d4b7db6b5a56d470))
* do not cache partial library pie results ([2a320b4](https://github.com/homeylab/tdarr-exporter/commit/2a320b4171e3504414e329146228de70c1348316))
* do not serve partial pie results from cache; scope partial-failure to one scrape ([e0ffa31](https://github.com/homeylab/tdarr-exporter/commit/e0ffa315466ccad13621c31bb7987be3c1f468e3))
* exit 128+signum on force-quit instead of 1 ([fb65fdf](https://github.com/homeylab/tdarr-exporter/commit/fb65fdf935aa50da40934dcf83464d31b2201421))
* exit 128+signum on force-quit instead of 1 ([c37f54f](https://github.com/homeylab/tdarr-exporter/commit/c37f54f5f12f60a0dba5bbd5c049667ad693896c))
* exit codes, config validation, and dead-code cleanup ([14e2a94](https://github.com/homeylab/tdarr-exporter/commit/14e2a94371ca5811b45027c9ff8937d713ffe009))
* exit non-zero when shutdown is caused by an HTTP server error ([68faacb](https://github.com/homeylab/tdarr-exporter/commit/68faacb4cf885ba7ddafe6f7063b1102d9f68215))
* invalidate stats cache when library list changes ([f0461aa](https://github.com/homeylab/tdarr-exporter/commit/f0461aae4061c535be3f690d42d3159cff69bc0d))
* invalidate stats cache when library list changes ([c875eb0](https://github.com/homeylab/tdarr-exporter/commit/c875eb050e8e5d9306b14dd7e298c5f1331a1a92))
* link landing page and 404 hint to configured prometheus_path ([02affdc](https://github.com/homeylab/tdarr-exporter/commit/02affdc480434b82a06cc84162824b51d915bba2))
* link landing page and 404 hint to configured prometheus_path ([aac83ff](https://github.com/homeylab/tdarr-exporter/commit/aac83ff198d9223b2594d611cfd3c1786b657a59))
* reject malformed prometheus_path before ServeMux registration ([edee50c](https://github.com/homeylab/tdarr-exporter/commit/edee50c4d248ae89736ccf05e0d30ef4fc3dff92))
* reject malformed prometheus_path before ServeMux registration ([1da99e5](https://github.com/homeylab/tdarr-exporter/commit/1da99e5674bcdc71a0f573ff58bc8d644cde9ed8))
* reject non-canonical prometheus_port and correct route-conflict comment ([d078646](https://github.com/homeylab/tdarr-exporter/commit/d07864678b0b8c08586b2ebcbae0d4160713bb50))
* restore response-body diagnostic on decode failure ([0e085e9](https://github.com/homeylab/tdarr-exporter/commit/0e085e9b9ff32b95fa3b777f7b3ce6acedefba65))
* restore response-body diagnostic on decode failure ([ee0f9ba](https://github.com/homeylab/tdarr-exporter/commit/ee0f9ba1a8f3c32fbb1a3fc584a4d9918d5a4b96))
* scope partial-failure signal to a single scrape ([ce4eed8](https://github.com/homeylab/tdarr-exporter/commit/ce4eed8668d0fef9104e33e38041a1fe850fba3b))
* startup validation and shutdown-path hardening ([ff3281f](https://github.com/homeylab/tdarr-exporter/commit/ff3281f0679085ca8d0961bc134ec2d6cf47b42f))
* startup validation and shutdown-path hardening ([d80f50b](https://github.com/homeylab/tdarr-exporter/commit/d80f50b0fcb3671532891cd1450881bf5d6b71b4))
* store cache totals and stats as one atomic pair ([b2c2611](https://github.com/homeylab/tdarr-exporter/commit/b2c26111569b2c39cddb16feed285f1aa8273ca8))
* use strconv.ParseUint for prometheus_port validation ([11a6be8](https://github.com/homeylab/tdarr-exporter/commit/11a6be8fd2f4f72c07b0a4752652b1fe52148713))
* validate prometheus_port/prometheus_path, register -version as a real flag ([3491969](https://github.com/homeylab/tdarr-exporter/commit/34919699c1798db82c81b57fdea5b86ad7c745c7))

## [3.0.0](https://github.com/homeylab/tdarr-exporter/compare/v2.1.0...v3.0.0) (2026-06-13)


### ⚠ BREAKING CHANGES

* the library_name label is removed from all tdarr_library_* metrics. Recover it by joining on library_id against the new tdarr_library_info{library_id, library_name} metric, e.g. `tdarr_library_files * on (library_id) group_left(library_name) tdarr_library_info`. Dashboards and alert rules that selected or grouped by library_name must be updated to join through tdarr_library_info (see examples/dashboard.json and examples/alerts.yaml).
* tdarr_score_pct, tdarr_health_check_score_pct, tdarr_node_host_cpu_percent and tdarr_node_worker_percentage are renamed to _ratio and now expose 0-1 values instead of 0-100. Update queries, alerts and dashboard panels accordingly.
* metric names and value scales changed; update dashboards and recording/alerting rules referencing the old *_mb/_gb names.
* tdarr_node_worker_start_timestamp_seconds renamed to tdarr_node_worker_step_start_timestamp_seconds. Disambiguates from the sibling job-start timestamp; "step" covers both classic plugin steps and flow steps.
* metric names and types changed; dashboards/alerts/recording rules referencing the old names must be updated.
* tdarr_node_worker_info no longer carries worker_status, worker_plugin_id, worker_plugin_position, or worker_idle labels. Queries and dashboards must read these from the new tdarr_node_worker_status, tdarr_node_worker_plugin, and tdarr_node_worker_idle metrics.
* tdarr_scrape_requests_total is removed. Queries/alerts must move to promhttp_metric_handler_requests_total (same {code} label). The "Handler Errors (5m)" dashboard panel is repointed accordingly.

### Features

* add build_info metric and --version via prometheus/common/version ([fd05d8f](https://github.com/homeylab/tdarr-exporter/commit/fd05d8fecc228fb516251099e1dc60f232725523))
* add ErrUpstream/ErrParse sentinels for collection failure causes ([f31e50c](https://github.com/homeylab/tdarr-exporter/commit/f31e50c2fe88aaca9e98fc5e893640d31a3d31df))
* add Exporter Internals panels and split Scrape Health row ([08f0e6f](https://github.com/homeylab/tdarr-exporter/commit/08f0e6fe873c0242bbf04fc32696b4af845c63b4))
* add GPU Select panel and clarify node panels ([ca012f8](https://github.com/homeylab/tdarr-exporter/commit/ca012f80454c3f7c23dae5ee0f535dd93b3d04cd))
* add GPU Select panel and dashboard layout pass ([90871fe](https://github.com/homeylab/tdarr-exporter/commit/90871fe4207747b0aba6a2724bbab931d593b652))
* add tdarr_server_healthy gauge and server info dashboard panels ([4f9255e](https://github.com/homeylab/tdarr-exporter/commit/4f9255e2cec17c580132225978835dc0ece5cb6c))
* add TdarrServerStatus model for /api/v2/status ([b391a0e](https://github.com/homeylab/tdarr-exporter/commit/b391a0eb99bb8c0cea78927823c07219c3455e80))
* add TdarrStatusPath config for /api/v2/status endpoint ([1a1c282](https://github.com/homeylab/tdarr-exporter/commit/1a1c2821c5271dffbd4d0722ad837a9966a7a308))
* canonical promhttp handler metrics + Exporter Internals dashboard ([2f026fa](https://github.com/homeylab/tdarr-exporter/commit/2f026faf67192c3e27f97c1115accb4b86a5db96))
* convert size/heap/mem metrics to base-unit bytes, duration to seconds ([b3b645b](https://github.com/homeylab/tdarr-exporter/commit/b3b645bee7cc792e953f585458aaf9296f5b282f))
* emit canonical promhttp handler metrics with tdarr_instance ([03a557d](https://github.com/homeylab/tdarr-exporter/commit/03a557da0615c77f683b305c00341eb4741d7285))
* emit tdarr_library_audio_resolutions metric ([7b7d042](https://github.com/homeylab/tdarr-exporter/commit/7b7d04248e584c2f21221ae10684f74032a664f4))
* expose go/process runtime metrics; modernize interface{} to any ([96431cc](https://github.com/homeylab/tdarr-exporter/commit/96431cc27ba47dfa53b422fc046e7e2558b8419c))
* fetch /api/v2/status and emit server uptime/info/status metrics ([0633bcd](https://github.com/homeylab/tdarr-exporter/commit/0633bcd2d8351ec3251c87b673f0c683a1dfb4cb))
* migrate releases to release-please ([7de843d](https://github.com/homeylab/tdarr-exporter/commit/7de843dc05ab2c1fa10aa45236385860b5f05dc8))
* migrate releases to release-please ([21f8300](https://github.com/homeylab/tdarr-exporter/commit/21f8300528480837e712a6629296abd0b41e9828))
* move library_name to tdarr_library_info, key library metrics on library_id ([b87a834](https://github.com/homeylab/tdarr-exporter/commit/b87a83446ef824aae7c7b9dc14b2b1379dcec47a))
* propagate context through scrape for shutdown cancellation ([e2960e9](https://github.com/homeylab/tdarr-exporter/commit/e2960e9f459f4c5de53bfb258d4c397366517f55))
* register server status/info/uptime descs in collector ([3cd5b1c](https://github.com/homeylab/tdarr-exporter/commit/3cd5b1c227a73badf2365d4411199ff330b7449b))
* rename percent metrics to _ratio (0-1), fix worker-table byte units ([89a7ad8](https://github.com/homeylab/tdarr-exporter/commit/89a7ad828ae5aa2562d4cbd15e1132098bff773f))
* rename worker step-start metric, fix size-diff semantics, surface orphan metrics ([1863480](https://github.com/homeylab/tdarr-exporter/commit/1863480abb3059f1959e416c18993ae81755ad3b))
* rename/retype *_total metrics for Prometheus naming (P3.1) ([2c75f04](https://github.com/homeylab/tdarr-exporter/commit/2c75f0463d388b0f26d18073abafc8f37f735d27))
* split worker status/plugin/idle off node_worker_info ([da84617](https://github.com/homeylab/tdarr-exporter/commit/da84617bdd4a5922ac450d4843a8cffe47b80c06))
* Tdarr server status metrics + dashboard panels ([e6e210c](https://github.com/homeylab/tdarr-exporter/commit/e6e210c982e402ed3120e7137c958c6af22404be))


### Bug Fixes

* check Close() errors in tests to satisfy errcheck lint ([37d6116](https://github.com/homeylab/tdarr-exporter/commit/37d61166c493a16da55b873eebcba5ab52abf789))
* classify post-retry 4xx/3xx as errors and detect URL scheme via :// ([ee9a31f](https://github.com/homeylab/tdarr-exporter/commit/ee9a31fc2e495f0fe2dfa02a223cdd0faeb1d13c))
* clone HTTP transport, guard cache read, make retry testable ([912d121](https://github.com/homeylab/tdarr-exporter/commit/912d121f9175f6b31e8a927d5491b3f4d4893ff0))
* close discarded response bodies in retry transport ([b407831](https://github.com/homeylab/tdarr-exporter/commit/b407831b3f1bb55181f29716913f9969b355a30d))
* collapse ci test pass into single test_all task ([50cda1b](https://github.com/homeylab/tdarr-exporter/commit/50cda1bd2bcce1bda08254cd7c77ac24b7384367))
* correct library_audio_containers metric help text ([c859d6e](https://github.com/homeylab/tdarr-exporter/commit/c859d6e70dd6faaed08bbd926db0342839f50973))
* drop tdarr_library_audio_resolutions; Tdarr audio pie has no resolutions ([bc37029](https://github.com/homeylab/tdarr-exporter/commit/bc370298a445a2145be31c14cf458272f6a75155))
* harden CI, build flags, and dev tooling ([c25daae](https://github.com/homeylab/tdarr-exporter/commit/c25daaeae3196215d1993791b1953f82a040ebfb))
* harden CI, build flags, and dev tooling ([22b690f](https://github.com/homeylab/tdarr-exporter/commit/22b690f3218688ee5637dc4e7e7a697a3998d25d))
* harden renovate config ([42e0915](https://github.com/homeylab/tdarr-exporter/commit/42e0915a49518cf81ca303386feeeb2bbf574c09))
* harden renovate config ([79f79d8](https://github.com/homeylab/tdarr-exporter/commit/79f79d80bebc706ec9ed91ab60f3b526e581d144))
* namespace Taskfile tasks with colon grouping ([d9301d5](https://github.com/homeylab/tdarr-exporter/commit/d9301d574cfdfb25b6025cad58b8412984841466))
* propagate server errors instead of os.Exit in goroutine ([15454b7](https://github.com/homeylab/tdarr-exporter/commit/15454b766ff298d1f828a131e0e5863140dcfe59))
* recover from scrape panics, degrade to tdarr_up=0 ([d2a9267](https://github.com/homeylab/tdarr-exporter/commit/d2a92679be2b001d8ff1d71111995e91129e5b26))
* recover from scrape panics, degrade to tdarr_up=0 ([507d79f](https://github.com/homeylab/tdarr-exporter/commit/507d79f7c38e2d746151ca17b364c5a042689d0a))
* remove dead tdarr_node_worker_pid metric ([8a26beb](https://github.com/homeylab/tdarr-exporter/commit/8a26beb5ddf8cfe499fe2e5ad3f90c5b7e06159e))
* remove dead tdarr_node_worker_pid metric ([de377b7](https://github.com/homeylab/tdarr-exporter/commit/de377b7dd8ef9251ef7a84c63fd304b271f49ab5))
* remove duplicate root renovate.json ([385816c](https://github.com/homeylab/tdarr-exporter/commit/385816cac40e17f5f9f860fbbe87529b27d5395b))
* remove duplicate root renovate.json ([996613a](https://github.com/homeylab/tdarr-exporter/commit/996613a5b2d7d5ab654309df70c121b5e2187658))
* run go mod tidy for renovate updates ([07a274e](https://github.com/homeylab/tdarr-exporter/commit/07a274e628d1b470c51aaa8ffb4e4b2f9e091a8f))
* run go mod tidy for renovate updates ([091694b](https://github.com/homeylab/tdarr-exporter/commit/091694bd9aac6897698792eca97286494875a208))
* scope transcode-failed alert join to include tdarr_instance ([2b22d59](https://github.com/homeylab/tdarr-exporter/commit/2b22d59101799c61a5f8fc398a36ffb9333b4837))
* split File Descriptors panel into used-vs-limit graph + %-of-limit stat ([4b7ec80](https://github.com/homeylab/tdarr-exporter/commit/4b7ec801f9445847d6d5474395a72e3814bae94d))
* split File Descriptors panel into used-vs-limit graph + %-of-limit stat ([f2a1973](https://github.com/homeylab/tdarr-exporter/commit/f2a1973359d16144379a7d42cef797bb2c6d7f77))
* use default-action + subcommand task naming ([dd32ca3](https://github.com/homeylab/tdarr-exporter/commit/dd32ca363ca8ccd6271af33832844963c1897104))


### Code Refactoring

* drop redundant tdarr_scrape_requests_total ([b976fe2](https://github.com/homeylab/tdarr-exporter/commit/b976fe21142614f2379a4276edac496d3a1f8e84))
