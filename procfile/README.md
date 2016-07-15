# Procfile

This is a Go library for parsing the Procfile format.

## Formats

### Standard

The standard Procfile format is what you're probably most familiar with, which maps a process name to the command to run. An example of a standard Procfile might look like:

```yaml
web: ./bin/web
worker: ./bin/worker
```

The standard Procfile format is specified in https://devcenter.heroku.com/articles/procfile.

### Extended

The extended Procfile format is Empire specific and implements a subset of the attributes defined in the [docker-compose.yml](https://docs.docker.com/compose/yml/) format. The extended Procfile format gives you more control, and allows you to configure additional settings. An example of an extended Procfile might look like:

```yaml
web:
  command: ./bin/web
scheduled-job:
  command: ./bin/scheduled-job
  cron: '0/2 * * * ? *'
```

#### Attributes

**Command**

Specifies the command that should be run when executing this process.

```yaml
command: ./bin/web
```

**Cron**

When provided, signifies that the process is a scheduled process. The value should be a valid cron expression.

```yaml
cron: * * * * * * // Run once every minute
```

**Ports**

This allows you to define what ports to expose, and what protocol to expose them with. This works similarly to the `ports:` attribute in docker-compose.yml.

```yaml
ports:
  # Map port 80 on the container, as port 80 on the load balancer, using the default protocol.
  - "80"
  # Map port 8080 on the container, as port 80 on the load balancer, using the default protocol.
  - "80:8080"
  # Map port 5678 on the container, as port 5678 on the load balancer, using the tcp protocol.
  - "5678":
      protocol: "tcp"
```
