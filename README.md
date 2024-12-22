# Self-hosted HiBP password hash check only

This is an example of self-hosted HiBP pwned password list API with the k-anonymity setting.

This code serves primarily as an example for setting up [self-hosted HiBP for Ory Kratos](https://github.com/ory/kratos/pull/1009#issuecomment-826372061) but there is nothing preventing it from running in a completely local environment where the access to the public HiBP API would be prevented but password checks are required.

## Supported databases

- PostgreSQL 13 is the only database this has been tested with and PostgreSQL is the only database supported

## Download the passwords SHA-1 ordered by count file

Download the SHA-1 7zip archive from HiBP website. The most up to date file can be downloaded from https://haveibeenpwned.com/Passwords. The compressed file is 12.5GB, 26GB decompressed. The file is a text file with each line in the following format:

```
SHA-SUM:INT-COUNT
```

On Ubuntu (requires ~40GB free space in `/tmp`):

```sh
sudo apt-get install p7zip-full
cd /tmp
wget https://downloads.pwnedpasswords.com/passwords/pwned-passwords-sha1-ordered-by-count-v7.7z
p7zip -d pwned-passwords-sha1-ordered-by-count-v7.7z
```

## Build the Docker image

```
make docker-build
```

A `localhost/hibp:latest` Docker image will be created.

## Start the example Docker compose environment

```sh
cd examples/compose/
docker-compose -f compose.yml up
```

## Create the table

The migrate command will create a partitioned table structure optimized for fast prefix lookups. Each partition corresponds to a two-character hex prefix (00-FF), with appropriate indexes.

```sh
docker run --rm \
    --net=compose_hibpexample \
    localhost/hibp:latest \
    migrate --dsn=postgres://hibp:hibp@postgres:5432/hibp?sslmode=disable
```

This command will:
1. Create the main `hibp` table partitioned by the first two characters of the hash
2. Create 256 partitions (one for each possible two-character hex prefix)
3. Create indexes on each partition for optimized prefix lookups

### Import the data

The data import process has been enhanced with several improvements:

- Parallel processing with configurable worker pool
- Batch processing for efficient database inserts
- Automatic retries for failed API requests
- Rate limiting to prevent API throttling
- Detailed error reporting and progress tracking

Basic import command:

```sh
docker run --rm \
    --net=compose_hibpexample \
    -v=/tmp/pwned-passwords-sha1-ordered-by-count-v7.txt:/tmp/pwned-passwords-sha1-ordered-by-count-v7.txt \
    localhost/hibp:latest \
    data-import --dsn=postgres://hibp:hibp@postgres:5432/hibp?sslmode=disable \
      --password-file=/tmp/pwned-passwords-sha1-ordered-by-count-v7.txt
```

Additional configuration options:

- `--batch-size=N`: Number of records to insert in one batch (default: 1,000,000)
- `--no-truncate`: Skip truncating the table before import
- `--workers=N`: Number of concurrent workers (default: 32)

The import process will:
1. Truncate the existing table (unless --no-truncate is specified)
2. Process the password file in parallel using multiple workers
3. Insert records in efficient batches
4. Display progress updates
5. Automatically retry failed requests with exponential backoff

## Test

If you import the data with `--first=X` flag, pick a hash from first X lines of the file, for example, for verion 7:

```sh
less /tmp/pwned-passwords-sha1-ordered-by-count-v7.txt
```

gives this in the first line:

```
7C4A8D09CA3762AF61E59520943DC26494F8941B:24230577
```

The `:prefix` in the `GET /range/:prefix` call is the first 5 characters of the hash, to query, execute:

```sh
curl http://localhost:15000/range/7C4A8
```

## Setting up behind reverse proxy with TLS

At the moment, Kratos does not allow providing a custom CA certificate to communicate with a custom HiBP API but it requires TLS. If a private certificate authority is required, the private CA chain can be installed on the operating system where Kratos is served from. Alternatively, a Let's Encrypt certificate can be issued to the HiBP application. The example contains the Traefik reverse proxy configured with an ACME LE resolver.

To configure the example:

- edit `examples/compose/compose.yml` file and set your actual host name the HiBP should be served in the ``Host(`your-hibp-api.example.com`)`` rule
- edit the `certificatesResolvers.hibp-certresolver.acme.email` setting in the `examples/compose/etc/traefik/traefik.toml` file to your LE registration email address

You can start the setup with the Traefik reverse proxy using the following command:

```sh
cd examples/compose/
docker-compose -f traefik.yml -f compose.yml up
```

## License

Apache-2.0 License
