# notifr

notifr is a simple HTTP server that resends incoming messages to configured delivery services.

The application allows you to aggregate messages from different services and sends them to recipients.
The server receives an HTTP request that contains a message and target. You can specify several recipients for each target and delivery methods in the notifr's configuration.
It is possible to use Markdown format messages in the requests, which are converted to HTML when sending emails.
notifr supports resending the email in the event of a failed attempt to send with a temporary net error (ECONNRESET and ECONNABORTED).

The main goal of this application is a simplification of other services that need to broadcast notifications.

## Features

- Messages in CommonMarkdown format with extensions that is provided provided by [blackfriday][blackfriday] library;
- Support SMTP delivery.

## Requirements

- SMTP Relay server (e.g. [postfix][postfix]).

## Installing

### From Docker

```bash
docker pull icoreru/notifr
```

### From sources

```bash
go install ./...
```

## Configuration

The application is configured via environment variables.
Names of the environment variables start with prefix `NOTIFR_`.
See a list of the environment variables using the command:

```bash
notifr -h
```

### Notification targets

Configuration of notification targets is comma-separated values with colons as row separators. Each target value has the next format `TargetName:DeliveryName:Recipient`.

## Notification

To notify you should send HTTP request:

```bash
curl -X POST -H 'Content-Type: application/json' -d 'REQUEST_BODY' http://localhost:8080/notifr?target=TARGET_NAME
```

A request body must follow to the next JSON scheme:

```yaml
type: object
properties:
    subject:
        type: string
    text:
        type: string
required:
    - text
```

Property `text` contains a message text in Markdown format (standard markdown syntax).
If the property `subject` not defined the first line from the `text` field is truncated to 78 characters and adding in the subject while sending an email.

## Example

Start the server:

```bash
docker run --name notifr -p 8080:8080             \
    -e NOTIFR_SMTP_HOST=smtp.example.org          \
    -e NOTIFR_TARGETS=test:smtp:email@example.org \
    icoreru/notifr
```

Send a notification:

### Notification with the subject field

```bash
curl -X POST -H 'Content-Type: application/json' -d '{"subject":"My first notification","text":"## A list\n- First\n- Second\n- Third"}' http://localhost:8080/notifr?target=test
```

Email:

```html
Subject: My first notification
Content-Type: text/html; charset=UTF-8

<h2>A list</h2>

<ul>
<li>First</li>
<li>Second</li>
<li>Third</li>
</ul>
```

### Notification without the subject field

```bash
curl -X POST -H 'Content-Type: application/json' -d '{"text":"# My first notification\n## A list\n\n- First\n- Second\n- Third"}' http://localhost:8080/notifr?target=test
```

Email:

```html
Subject: My first notification
Content-Type: text/html; charset=UTF-8

<h1>My first notification</h1>

<h2>A list</h2>

<ul>
<li>First</li>
<li>Second</li>
<li>Third</li>
</ul>
```

## Contributing

Thanks for your interest in contributing to this project.
Get started with our [Contributing Guide][contrib].

## License

The code in this project is licensed under [MIT license][license].

[contrib]: https://github.com/i-core/.github/blob/master/CONTRIBUTING.md
[license]: LICENSE
[postfix]: https://hub.docker.com/r/juanluisbaptiste/postfix/
[blackfriday]: https://github.com/russross/blackfriday/tree/v2#extensions
