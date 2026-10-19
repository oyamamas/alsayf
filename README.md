# Third-party components

- [Gobuster v3.8.2](https://github.com/OJ/gobuster/tree/v3.8.2): the upstream CLI command factories and matching discovery engines for `dir`, `dns`, `vhost`, `fuzz`, `s3`, `gcs`, and `tftp`, imported as Go packages under `alsayf gbuster`. Integration details and the upstream license are retained in [third_party/gobuster](third_party/gobuster/).
- [urfave/cli v2](https://github.com/urfave/cli): Gobuster's native mode and option parsing, help, and CLI execution inside the module adapter.
- [CloudExec](https://github.com/oyamamas/CloudExec/tree/7390f34e3987809320836b82dfc5b43853dfb306), based on [cotsom/CloudExec](https://github.com/cotsom/CloudExec): the twelve service commands, service modules, target helpers, and exporter secret-detection rules. Runtime sources are adapted into `cmd/clx` and `internal/clx`, with fresh command constructors and registration under `clx`. Source revision and adaptation notes are recorded in [third_party/cloudexec/NOTICE](third_party/cloudexec/NOTICE).
- [pgx](https://github.com/jackc/pgx), [clickhouse-go](https://github.com/ClickHouse/clickhouse-go), [go-redis](https://github.com/redis/go-redis), [kafka-go](https://github.com/segmentio/kafka-go), [go-zookeeper](https://github.com/go-zookeeper/zk), and [etcd clients](https://github.com/etcd-io/etcd): CloudExec's service protocol clients.
- [go-git](https://github.com/go-git/go-git) and [go-billy](https://github.com/go-git/go-billy): Git repository and filesystem operations used by CloudExec's GitLab runner module.
- [goquery](https://github.com/PuerkitoBio/goquery): HTML parsing for CloudExec's exporter discovery.
- [Titus v1.1.33](https://github.com/praetorian-inc/titus/tree/v1.1.33): local matching engine (`pkg/matcher`), embedded detection rules and their loader (`pkg/rule`), and match types (`pkg/types`). The selected rules have the `secret` category and are not marked `noisy`.
- [Nosey Parker](https://github.com/praetorian-inc/noseyparker) and [Kingfisher](https://github.com/mongodb/kingfisher): detection patterns and rule metadata included through Titus. Upstream license and attribution notices are preserved in [third_party/titus](third_party/titus/).
- [Cobra](https://github.com/spf13/cobra): command hierarchy, help output, argument validation, and command execution.
- [pflag](https://github.com/spf13/pflag): command-line flag parsing through Cobra.
- [regexp2](https://github.com/dlclark/regexp2): portable regular-expression matching through Titus.
- [yaml.v3](https://gopkg.in/yaml.v3): parsing Titus's embedded YAML rule definitions.
- [gohs](https://github.com/flier/gohs) and [ahocorasick](https://github.com/cloudflare/ahocorasick): dependencies of Titus's optional accelerated matching backend; excluded from the default pure-Go build.
- [mousetrap](https://github.com/inconshreveable/mousetrap): Windows shell-launch detection through Cobra.
