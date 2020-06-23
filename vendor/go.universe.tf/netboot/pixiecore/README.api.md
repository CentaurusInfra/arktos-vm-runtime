# API server

Pixiecore supports two modes of operation: static and API-driven.

In static mode, you just pass it a -kernel and a -initrd, and
Pixiecore will boot any PXE client that it sees.

In API mode, requests made by PXE clients will be translated into
calls to an HTTP API, essentially asking "Should I boot this client?
If so, what should I boot it with?" This lets you implement fancy
dynamic booting things, as well as construct per-machine commandlines
and whatnot.

## API specification

Note that Pixiecore is the _client_ of this API. It's your job to
implement it and point Pixiecore at the right URL prefix.

The API consists of a single endpoint:
`<apiserver-prefix>/v1/boot/<mac-addr>`. Pixiecore calls this endpoint
to learn whether/how to boot a machine with a given MAC address.

Any non-200 response from the server will cause Pixieboot to ignore
the requesting machine.

A 200 response will cause Pixiecore to boot the requesting machine. A
200 response must come with a JSON document conforming to the
following specification, with **_italicized_** entries being optional:

- **kernel** (string): the URL of the kernel to boot.
- **_initrd_** (list of strings): URLs of initrds to load. The kernel
  will flatten all the initrds into a single filesystem.
- **_cmdline_** (string): commandline parameters for the kernel. The
  commandline is processed by Go's text/template library. Within the
  template, a `URL` function is available that takes a URL and
  rewrites it such that Pixiecore proxies the request.
- **_message_** (string): A message to display before booting the
  provided configuration. Note that displaying this message is on
  a _best-effort basis only_, as particular implementations of the
  boot process may not support displaying text.

Malformed 200 responses will have the same result as a non-200
response - Pixiecore will ignore the requesting machine.

### Kernel, initrd and cmdline URLs

As described above, the kernel and initrds are specified as URLs,
enabling you to host them as you please - you could even link directly
to a distro's download links if you wanted.

URLs provided by the API server can be absolute, or just a naked
path. In the latter case, the path is resolved with reference to the
API server URL that Pixiecore is using - although note that the path
is _not_ rooted within Pixiecore's API path. For example, if you
provide `/foo` as a URL to Pixiecore running with `api
http://bar.com/baz`, Pixiecore will fetch `http://bar.com/foo`, _not_
`http://bar.com/baz/foo`.

In addition to `http` and `https` URLs, Pixiecore supports `file://`
URLs to serve files from the filesystem of the machine running
Pixiecore. You can use this to host large OS images near the target
machines, while still deciding what to boot from a central but remote
location. Pixiecore uses the "path" segment of the URL, so all
`file://` URLs are absolute filesystem paths.

Pixiecore will not point booting machines directly at the given
URLs. Instead, it will point the booting machines to a proxy URL on
Pixiecore's HTTP server, and proxy the transfer.

This is done for two reasons: one, the booting machine may be in a
restricted network environment. For example, you may have a policy
that machines must do 802.1x authentication to get full network
access, else they get dropped on a "remediation" vlan. Proxying the
downloads through Pixiecore means you need only one set of edge ACLs
on the remediation vlan, regardless of _what_ you're booting: just
whitelist Pixiecore's IP:port, and from there your API server can boot
whatever you want.

Second, the booting machine is limited to using HTTP to fetch
images. This is probably okay (though not ideal, admittedly - but then
again, PXE forces us to TFTP anyway, so we're already screwed for
security) on the machine's local ethernet broadcast domain, but is
definitely not okay for retrieval over the internet. Proxying through
Pixiecore means that your API server can provide HTTPS URLs, and
everything but the very last mile between Pixiecore and the machine
will be secure.

The exact URLs visible to the booting machine are an implementation
detail of Pixiecore and are subject to breaking change at any
time.

For the curious, the current implementation translates API server
provided URLs into `<pixiecore HTTP endpoint>/_/file?name=<signed URL
blob>`. The signed URL blob is a base64-encoding of running NaCL's
secretbox authenticated encryption function over the server-provided
URL, using an ephemeral key generated when Pixiecore starts. This
steers the booting machine through Pixiecore for the fetch, and lets
Pixiecore verify that it's only proxying for URLs that the API server
gave it, so it's not an open proxy on your remediation vlan.

### Multiple calls

Pixiecore in API mode is stateless. Due to the unique way that PXE
works, the API server may receive multiple requests for a single
machine boot. Unfortunately, there is no good way to reliably provide
a 1:1 mapping between a machine boot and an API server request.

If you want to implement "single-shot" boot behavior (i.e. "netboot
this MAC once, then go back to ignoring it"), you'll need to add a
signalling backchannel to the OS image, so that it signals your API
server when it's booted. Responding only to the first request for a
MAC address will not have the desired effect.

### Example responses

Boot into CoreOS stable. **WARNING**: this example is **unsafe**,
because the images are linked to over HTTP, and we're not doing GPG
verification of the image signatures. This is an example only.

```json
{
  "kernel": "http://stable.release.core-os.net/amd64-usr/current/coreos_production_pxe.vmlinuz",
  "initrd": ["http://stable.release.core-os.net/amd64-usr/current/coreos_production_pxe_image.cpio.gz"]
}
```

Boot from API server provided files. Pixiecore will grab kernel and
initrd from `<apiserver-host>/kernel` and `<apiserver-host>/initrd.[01]`.

```json
{
  "kernel": "/kernel",
  "initrd": ["/initrd.0", "/initrd.1"]
}
```

Boot from HTTPS, with extra commandline flags.

```json
{
  "kernel": "https://files.local/kernel",
  "initrd": ["https://files.local/initrd"],
  "cmdline": "selinux=1 coreos.autologin"
}
```

Boot from Pixiecore's local filesystem.

```json
{
  "kernel": "file:///mnt/data/kernel",
  "initrd": ["file:///mnt/data/initrd"],
}
```

Provide a proxied cloud-config and an unproxied other URL.

```json
{
  "kernel": "https://files.local/kernel",
  "initrd": ["https://files.local/initrd"],
  "cmdline": "cloud-config-url={{ URL \"https://files.local/cloud-config\" }} non-proxied-url=https://files.local/something-else"
}
```

### Example API server

There is a very small example API server implementation in the
`api-example` subdirectory. This sample server is not production-quality
code (e.g. it uses panic for error handling), but should be a
reasonable starting point nonetheless. It will instruct pixecore
to boot Tiny Core Linux' kernel and initrd(s),
directly from upstream servers.

## Advanced, non-guaranteed features

### Custom iPXE boot script

Pixiecore aims to abstract away the details of the network boot
process. This gives it the freedom to adjust the exact sequence of
events as more firmware bugs and quirks are discovered in the wild,
and simplifies API design because you only have to specify what you
want the _end_ state to be, not manage the intermediate stages.

However, in some use cases, you may want to do advanced things within
the bootloader, prior to booting the OS. For these cases, the API
supports passing a raw iPXE boot script.

Please note that **this is not a stable interface**, and never will
be. iPXE is an implementation detail of Pixiecore's boot process, and
may change at some point in the future without warning. Additionally,
new booting methods may be added that don't use iPXE at all (for
example, the new RedFish machine management APIs).

By using this advanced feature, you understand that this may break in
future versions of Pixiecore. It's up to you to verify that your use
case continues to work as Pixiecore updates. We still welcome bug
reports if you're using this feature, just be aware that some of them
may end up being "won't fix, working as intended."

Additionally, note that it is _your_ responsibility to successfully
complete the boot process, Pixiecore's involvement ends with serving
your iPXE script. Pixiecore does no sanity checking on your script, it
just hands it verbatim to iPXE.

If you're okay with this disclaimer, you can use this functionality by
just passing an `ipxe-script` element as the only element of your
response:

```json
{
  "ipxe-script": "#!ipxe\nyour-ipxe-script-here"
}
```

## Deprecated features

### Kernel commandline as an object

The `cmdline` parameter returned by the API server can also be a JSON
object, where each key/value pair is one commandline parameter. The
format is:

- **_cmdline_** (object): commandline parameters for the kernel. Each
  key/value pair maps to key=value, where value can be:
  - **string**: the value is passed verbatim to the kernel
  - **true**: the value is omitted, only the key is passed to the
    kernel.
  - **object**: the value is a URL that Pixiecore will rewrite such
    that it proxies the request (see below for why you'd want that).
    - **url** (string): any URL. Pixiecore will rewrite the URL such
      that it proxies the request.

This form was replaced by the use of Go's text/template to allow for
inline URL substitution, without having to construct a cumbersome JSON
object, and to resolve issues with non-deterministic commandline
parameter ordering.

There are currently no plans to remove this form of the `cmdline`
value, though its use is discouraged.
