package policyutil

import (
	"net/netip"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
)

// ReduceFilterRules takes a node and a set of global filter rules and removes all rules
// and destinations that are not relevant to that particular node.
//
// IMPORTANT: This function is designed for global filters only. Per-node filters
// (from autogroup:self policies) are already node-specific and should not be passed
// to this function. Use PolicyManager.FilterForNode() instead, which handles both cases.
func ReduceFilterRules(node types.NodeView, rules []tailcfg.FilterRule) []tailcfg.FilterRule {
	ret := []tailcfg.FilterRule{}

	for _, rule := range rules {
		// record if the rule is actually relevant for the given node.
		var dests []tailcfg.NetPortRange
		var capGrants []tailcfg.CapGrant

	DEST_LOOP:
		for _, dest := range rule.DstPorts {
			expanded, err := util.ParseIPSet(dest.IP, nil)
			// Fail closed, if we can't parse it, then we should not allow
			// access.
			if err != nil {
				continue DEST_LOOP
			}

			if node.InIPSet(expanded) {
				dests = append(dests, dest)
				continue DEST_LOOP
			}

			// If the node exposes routes, ensure they are note removed
			// when the filters are reduced.
			if node.Hostinfo().Valid() {
				routableIPs := node.Hostinfo().RoutableIPs()
				if routableIPs.Len() > 0 {
					for _, routableIP := range routableIPs.All() {
						if expanded.OverlapsPrefix(routableIP) {
							dests = append(dests, dest)
							continue DEST_LOOP
						}
					}
				}
			}

			// Also check approved subnet routes - nodes should have access
			// to subnets they're approved to route traffic for.
			subnetRoutes := node.SubnetRoutes()

			for _, subnetRoute := range subnetRoutes {
				if expanded.OverlapsPrefix(subnetRoute) {
					dests = append(dests, dest)
					continue DEST_LOOP
				}
			}
		}

		for _, grant := range rule.CapGrant {
			var matchedDsts []netip.Prefix

			for _, dst := range grant.Dsts {
				if grantMatchesNodePrefix(node, dst) {
					matchedDsts = append(matchedDsts, dst)
				}
			}

			if len(matchedDsts) > 0 {
				capGrants = append(capGrants, tailcfg.CapGrant{
					Dsts:   matchedDsts,
					Caps:   grant.Caps,
					CapMap: grant.CapMap,
				})
			}
		}

		if len(dests) > 0 || len(capGrants) > 0 {
			ret = append(ret, tailcfg.FilterRule{
				SrcIPs:   rule.SrcIPs,
				DstPorts: dests,
				IPProto:  rule.IPProto,
				CapGrant: capGrants,
			})
		}
	}

	return ret
}

func prefixesOverlap(a, b netip.Prefix) bool {
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func grantMatchesNodePrefix(node types.NodeView, dst netip.Prefix) bool {
	if node.InIPSet(mustIPSetFromPrefix(dst)) {
		return true
	}

	if node.Hostinfo().Valid() {
		routableIPs := node.Hostinfo().RoutableIPs()
		if routableIPs.Len() > 0 {
			for _, routableIP := range routableIPs.All() {
				if prefixesOverlap(dst, routableIP) {
					return true
				}
			}
		}
	}

	for _, subnetRoute := range node.SubnetRoutes() {
		if prefixesOverlap(dst, subnetRoute) {
			return true
		}
	}

	return false
}

func mustIPSetFromPrefix(prefix netip.Prefix) *netipx.IPSet {
	var builder netipx.IPSetBuilder
	builder.AddPrefix(prefix)
	set, _ := builder.IPSet()
	return set
}
