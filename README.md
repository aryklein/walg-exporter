# infra-data-walg-exporter

A straightforward Go exporter designed to verify WAL files backed up using
[WAL-G](https://github.com/wal-g/wal-g) and expose the results to Prometheus.

## How to build the exporter

```bash
go-task build
```

## How to build the container image

The container image for this exporter is built and pushed the registry using
Github Actions. Alternatively, you can build it locally by executing the command
below:

```bash
go-task container
```

## How to release a new version

The version information is stored in the VERSION file. To release a new version,
follow these steps:

```bash
echo "1.0.1" > VERSION
go-task release
```

This process will generate a new tag v1.0.1 and push it to the Git repository,
which in turn triggers a new GitHub Action to compile and deploy the container
image.

## How to run the exporter

his exporter is engineered to operate within a Kubernetes cluster, utilizing the
sidecar Vault Injector for assistance. The essential secrets required for the
exporter's operation are securely stored in Vault and are dynamically injected
into the exporter's environment.


## Configuration

The exporter is configured using environment variables:

- `WALG_EXPORTER_PORT`: The port where the exporter will listen for requests.
- `WALG_EXPORTER_TIMER`: The time between each verification. (default: 30m)
- `ENV`: The environment where the exporter is running. (default: dev)
- `PGCLUSTERS`: A comma-separated list of Postgres clusters to verify.

Take the `.env` file as an example

## How to add more clusters in the scrapping list

Add the new cluster to the comma-separeted list `PGCLUSTERS` and restart
the deployment, that will make Vault Injector to inject the new secret into the
exporter.

## Exported metrics

The exporter exposes the following metrics:

- `wal_g_verify_integrity_status`: wal-g wal-verify integrity status - 0: OK, 1:
  Error, 2: Unknown
- `wal_g_show_status`: wal-g wal-show status - 0: OK, 1: Error, 2: Unknown
- `wal_g_backup_count`: number of base backups found
- `wal_g_last_upload_file_s3_timestamp`: last upload file timestamp UNIX epoch

## Limitations

- The exporter only supports S3 backups with the directory with the following
  structure: `s3://bucket-name/postgres/walg/cluster-name-env/`
