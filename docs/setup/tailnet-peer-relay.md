# Self-Hosted Tailnet Deployment Record

This document records the working deployment and validation state for the self-hosted Headscale environment behind `https://<HEADSCALE_DOMAIN>`.

## Topology

- Control plane:
  - `<HEADSCALE_DOMAIN>` -> cloud load balancer -> `<HEADSCALE_BACKEND_PUBLIC_IP>:8080`
- Headscale server:
  - public host: `<HEADSCALE_BACKEND_PUBLIC_IP>`
  - Tailscale IP: `100.64.0.2`
  - role: `exit node`
- Peer relay and subnet router:
  - public host: `<RELAY_PUBLIC_IP>`
  - private LAN IP: `<RELAY_LAN_IP>`
  - Tailscale IP: `100.64.0.3`
  - roles: `peer relay`, `subnet router`
- Private node behind subnet router:
  - private LAN IP: `<PRIVATE_NODE_LAN_IP>`
  - Tailscale IP: `100.64.0.4`

## Current roles

- `100.64.0.2`
  - tagged with `tag:exit`
  - advertises `0.0.0.0/0` and `::/0`
- `100.64.0.3`
  - tagged with `tag:relay`
  - tagged with `tag:subnet-router`
  - advertises `<PRIVATE_SUBNET_CIDR>`
  - runs relay server on `UDP 40000`
- `100.64.0.4`
  - normal node

## Headscale configuration

- Headscale binary:
  - custom peer-relay build based on commit `efd83da14e71a5cffee54d41edb86694ab14642d`
- Public control URL:
  - `https://<HEADSCALE_DOMAIN>`
- Backend listener:
  - `0.0.0.0:8080`
- Active config:
  - `/etc/headscale/config.yaml`
- Active policy:
  - `/etc/headscale/policy.hujson`

## Policy summary

The active policy contains:

- `group:admin` -> `wangrzneu@`
- `tagOwners`
  - `tag:relay`
  - `tag:subnet-router`
  - `tag:exit`
- `autoApprovers.routes`
  - `<PRIVATE_SUBNET_CIDR>` -> `tag:subnet-router`
- `autoApprovers.exitNode`
  - `tag:exit`
- permissive ACLs for current admin/tagged-role setup
- `grants`
  - all nodes may use nodes tagged `tag:relay` for `tailscale.com/cap/relay`

## Required ports

- Load balancer:
  - `TCP 443` public HTTPS for `<HEADSCALE_DOMAIN>`
  - must support `/ts2021` upgrade and long-lived `/machine/map` connections
- Headscale backend:
  - `TCP 8080` from load balancer to `<HEADSCALE_BACKEND_PUBLIC_IP>`
- Peer relay host `<RELAY_PUBLIC_IP>`:
  - `UDP 40000` public inbound

Important:

- The relay port is `40000/udp`, not `4000/udp`.

## Routing and forwarding

Both routing-capable nodes persist forwarding via:

- `/etc/sysctl.d/99-tailscale-routing.conf`

Current values:

- `net.ipv4.ip_forward = 1`
- `net.ipv6.conf.all.forwarding = 1`

## Validated behavior

- All nodes use `https://<HEADSCALE_DOMAIN>` as `ControlURL`
- Restarting `headscale` no longer strands clients on the old `HTTP:8080` reconnect path
- `100.64.0.3` is available as a peer relay candidate on all nodes
- `<PRIVATE_SUBNET_CIDR>` is approved and reachable through `100.64.0.3`
- `100.64.0.2` is advertised and approved as an exit node
- `100.64.0.2 <-> 100.64.0.4` can fall back to `peer-relay(<RELAY_PUBLIC_IP>:40000)` instead of public DERP when direct UDP is unavailable

## Change summary

This deployment combined four changes in order:

1. Move the control plane from `http://<HEADSCALE_BACKEND_PUBLIC_IP>:8080` to `https://<HEADSCALE_DOMAIN>`
2. Restore a formal policy file, then extend it with `grants` for peer relay
3. Redeploy the custom Headscale build that supports the minimal `grants + cap/relay` path
4. Configure node roles:
   - `100.64.0.2` as exit node
   - `100.64.0.3` as peer relay and subnet router

## Operational SOP

Apply or rebuild this setup in the following order:

1. Ensure the public control plane is exposed on `HTTPS 443`
   - `<HEADSCALE_DOMAIN>` must terminate TLS and forward to `<HEADSCALE_BACKEND_PUBLIC_IP>:8080`
2. Set `server_url` to `https://<HEADSCALE_DOMAIN>`
3. Restart `headscale`
4. Re-auth all clients against the new login server
5. Deploy the custom Headscale binary with peer-relay support
6. Apply the policy file containing:
   - `tagOwners`
   - `autoApprovers`
   - `grants` for `tailscale.com/cap/relay`
7. Tag nodes on the control plane
   - node `2` -> `tag:exit`
   - node `3` -> `tag:relay,tag:subnet-router`
8. On `100.64.0.2`
   - enable IP forwarding
   - `tailscale set --advertise-exit-node=true`
9. On `100.64.0.3`
   - enable IP forwarding
   - `tailscale set --advertise-routes=<PRIVATE_SUBNET_CIDR>`
   - `tailscale set --relay-server-port=40000 --relay-server-static-endpoints=<RELAY_PUBLIC_IP>:40000`
10. Open `UDP 40000` inbound on `<RELAY_PUBLIC_IP>`
11. Verify:
   - `sudo headscale nodes list`
   - `sudo headscale nodes list-routes`
   - `sudo tailscale debug peer-relay-servers`
   - `sudo tailscale debug peer-relay-sessions`

## Rollback SOP

If the custom peer-relay build must be rolled back:

1. Restore the stock `headscale` binary
2. Replace `/etc/headscale/policy.hujson` with a policy that does not contain `grants`
3. Restart `headscale`
4. Keep `https://<HEADSCALE_DOMAIN>` as the control URL
5. Leave node roles in place unless they are actively causing issues

If the HTTPS control plane is healthy, rollback should not require reauth of clients.

## Troubleshooting checklist

If clients fail to reconnect after `headscale` restart:

- Check they use `https://<HEADSCALE_DOMAIN>`, not `http://<HEADSCALE_BACKEND_PUBLIC_IP>:8080`
- Check the load balancer supports long-lived `/machine/map` traffic
- Check the load balancer does not return repeated `502`

If peer relay candidates are missing:

- Check the active policy contains `grants`
- Check the relay node has `tag:relay`
- Check `sudo tailscale debug peer-relay-servers`

If peer relay candidates exist but traffic still uses DERP:

- Check `UDP 40000` really reaches `<RELAY_PUBLIC_IP>`
- Check the relay node is listening on `40000`
- Check `sudo tailscale debug peer-relay-sessions`
- Remember that direct UDP is preferred over peer relay; peer relay only appears when direct connectivity is unavailable

If subnet routing fails:

- Check `100.64.0.3` advertises `<PRIVATE_SUBNET_CIDR>`
- Check the route is approved in `headscale nodes list-routes`
- Check the client has `accept-routes=true`

If exit node selection fails:

- Check `100.64.0.2` advertises `0.0.0.0/0` and `::/0`
- Check `headscale nodes list-routes`
- Check the client sees `ExitNodeOption: true` for `100.64.0.2`

## Peer relay validation method

Peer relay only becomes visible when direct node-to-node UDP is unavailable. The successful validation sequence was:

1. Confirm `100.64.0.3` is a candidate relay:
   - `sudo tailscale debug peer-relay-servers`
2. Confirm relay server is listening on `<RELAY_PUBLIC_IP>:40000/udp`
3. Confirm public reachability of `40000/udp`
4. Temporarily block direct UDP between `<HEADSCALE_BACKEND_PUBLIC_IP>` and `<PRIVATE_NODE_PUBLIC_IP>`
5. Rebind and retest with `tailscale ping`

Successful result:

- `via peer-relay(<RELAY_PUBLIC_IP>:40000:vni:6)`

## Useful commands

Check control URL:

```bash
sudo tailscale debug prefs | jq -r '.ControlURL'
```

Check relay candidates:

```bash
sudo tailscale debug peer-relay-servers
```

Check relay server sessions on the relay node:

```bash
sudo tailscale debug peer-relay-sessions
```

Check routes:

```bash
sudo headscale nodes list-routes
```

Check current policy:

```bash
sudo headscale policy get
sudo headscale policy check --file /etc/headscale/policy.hujson
```

Check subnet route use from `100.64.0.2`:

```bash
ip route get <PRIVATE_NODE_LAN_IP>
ping -c 3 <PRIVATE_NODE_LAN_IP>
```

Check exit node availability from a client:

```bash
sudo tailscale status --json | jq -r '
  .Peer | to_entries[]
  | select(.value.TailscaleIPs[]? == "100.64.0.2")
  | {HostName:.value.HostName, ExitNodeOption:.value.ExitNodeOption, AllowedIPs:.value.AllowedIPs}
'
```

## Known caveat

If direct connectivity is still available, Tailscale will keep preferring direct paths. This is expected. Peer relay is a fallback path that is used when direct transport cannot be established, before falling back to DERP.

## Suggested commit message

```text
docs: add runc.ai headscale peer-relay deployment runbook
```

Suggested commit body:

```text
- document the HTTPS control plane behind `<HEADSCALE_DOMAIN>`
- record peer relay, subnet router, and exit node roles
- capture required ports, validation commands, rollback steps, and troubleshooting notes
```

## Appendix: Measured Results

### Peer relay path validation

The peer relay path was validated by temporarily blocking direct UDP between:

- `<HEADSCALE_BACKEND_PUBLIC_IP>`
- `<PRIVATE_NODE_PUBLIC_IP>`

After forcing a rebind/restun, `tailscale ping` switched from DERP to peer relay:

- `100.64.0.2 -> 100.64.0.4`
  - `via peer-relay(<RELAY_PUBLIC_IP>:40000:vni:6)`
- `100.64.0.4 -> 100.64.0.2`
  - `via peer-relay(<RELAY_LAN_IP>:40000:vni:6)`

Observed fallback behavior:

- first packets may briefly use DERP
- once relay path setup completes, subsequent packets stabilize on peer relay

Observed RTT on the peer relay path:

- about `193-194 ms`

### Peer relay traffic counters

During the forced peer relay `iperf3` run, the relay node reported one active session:

```text
VNI: 6
<PRIVATE_NODE_LAN_IP>:41641 --> <HEADSCALE_BACKEND_PUBLIC_IP>:41641, Packets: 103532 Bytes: 120888596
<HEADSCALE_BACKEND_PUBLIC_IP>:41641 --> <PRIVATE_NODE_LAN_IP>:41641, Packets: 112157 Bytes: 130512712
```

### iperf3 over peer relay

Path under test:

- source: `100.64.0.2`
- destination: `100.64.0.4`
- relay: `100.64.0.3`
- mode: TCP, `4` parallel streams, `10s`

Forward test:

- command:
  - `iperf3 -c 100.64.0.4 -P 4 -t 10`
- result:
  - sender: `85.7 Mbits/sec`
  - receiver: `80.9 Mbits/sec`

Reverse test:

- command:
  - `iperf3 -c 100.64.0.4 -P 4 -t 10 -R`
- result:
  - sender: `93.2 Mbits/sec`
  - receiver: `84.4 Mbits/sec`

Interpretation:

- peer relay throughput is materially lower than the direct path
- in this environment, peer relay sustained about `80-90 Mbps`
- the peer relay path remained usable and significantly better than DERP fallback for this test pair
