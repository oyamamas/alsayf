# Third-party components

## Integrated tools

| Upstream tool | Alsayf command | Reused components |
| --- | --- | --- |
| [Titus v1.1.33](https://github.com/praetorian-inc/titus/tree/v1.1.33) | `alsayf secrets` | Portable local matching engine, 223 selected embedded secret-detection rules, rule loader, and match types. [License and attribution](third_party/titus/). |
| [CloudExec](https://github.com/oyamamas/CloudExec/tree/7390f34e3987809320836b82dfc5b43853dfb306), based on [cotsom/CloudExec](https://github.com/cotsom/CloudExec) | `alsayf clx` | Twelve service commands, service modules, target helpers, and exporter secret-detection rules, adapted into the toolkit. [Source and adaptation notes](third_party/cloudexec/NOTICE). |
| [Gobuster v3.8.2](https://github.com/OJ/gobuster/tree/v3.8.2) | `alsayf gbuster` | Native CLI command factories and discovery engines for `dir`, `dns`, `vhost`, `fuzz`, `s3`, `gcs`, and `tftp`. [License and adaptation notes](third_party/gobuster/). |
| [Nuclei v3.11.1](https://github.com/projectdiscovery/nuclei/tree/v3.11.1) | `alsayf nuclei` | Public template parser, catalog/loader, protocol implementations, and execution engine; local setup adapted from the upstream SDK. [License and adaptation notes](third_party/nuclei/NOTICE). |

## Supporting libraries and rule sources

- [Nosey Parker](https://github.com/praetorian-inc/noseyparker) and [Kingfisher](https://github.com/mongodb/kingfisher): detection patterns and rule metadata included through Titus. The selected Titus rules have the `secret` category and are not marked `noisy`. Upstream license and attribution notices are preserved in [third_party/titus](third_party/titus/).
- [ProjectDiscovery goflags](https://github.com/projectdiscovery/goflags), [gologger](https://github.com/projectdiscovery/gologger), and [ratelimit](https://github.com/projectdiscovery/ratelimit): Nuclei's native option value types, diagnostics, and request rate limiting. [miekg/dns](https://github.com/miekg/dns) reads system DNS resolver configuration; [Aurora](https://github.com/logrusorgru/aurora) supplies Nuclei's output colorizer interface with colors disabled.
- [urfave/cli v2](https://github.com/urfave/cli): Gobuster's native mode and option parsing, help, and CLI execution inside the module adapter.
- [pgx](https://github.com/jackc/pgx), [clickhouse-go](https://github.com/ClickHouse/clickhouse-go), [go-redis](https://github.com/redis/go-redis), [kafka-go](https://github.com/segmentio/kafka-go), [go-zookeeper](https://github.com/go-zookeeper/zk), and [etcd clients](https://github.com/etcd-io/etcd): CloudExec's service protocol clients.
- [go-git](https://github.com/go-git/go-git) and [go-billy](https://github.com/go-git/go-billy): Git repository and filesystem operations used by CloudExec's GitLab runner module.
- [goquery](https://github.com/PuerkitoBio/goquery): HTML parsing for CloudExec's exporter discovery.
- [Cobra](https://github.com/spf13/cobra): command hierarchy, help output, argument validation, and command execution.
- [pflag](https://github.com/spf13/pflag): command-line flag parsing through Cobra.
- [regexp2](https://github.com/dlclark/regexp2): portable regular-expression matching through Titus.
- [yaml.v3](https://gopkg.in/yaml.v3): parsing Titus's embedded YAML rule definitions.
- [gohs](https://github.com/flier/gohs) and [ahocorasick](https://github.com/cloudflare/ahocorasick): dependencies of Titus's optional accelerated matching backend; excluded from the default pure-Go build.
- [mousetrap](https://github.com/inconshreveable/mousetrap): Windows shell-launch detection through Cobra.
