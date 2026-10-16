package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// portFieldKind is the Terraform type of a port attribute and, with it, how the
// attribute's value is encoded in a port_overrides entry.
type portFieldKind int

const (
	portFieldString portFieldKind = iota
	portFieldBool
	portFieldInt
	portFieldStringSet
	portFieldIntSet
	// portFieldObject groups related keys of the entry into one nested
	// attribute, the shape unifi_device.port_override uses. Each child maps
	// onto its own key of the entry.
	portFieldObject
)

// portField maps one unifi_device_ports port attribute onto its key in the
// controller's port_overrides entry. The table below is the single source for
// the schema, the state read from the controller, and the keys written back.
// An object field has no apiKey of its own; its children carry the keys.
type portField struct {
	name         string
	apiKey       string
	kind         portFieldKind
	description  string
	oneOf        []string
	computedOnly bool
	children     []portField
}

var portFields = []portField{
	{
		name:        "name",
		apiKey:      "name",
		kind:        portFieldString,
		description: "Port name shown in the UniFi UI.",
	},
	{
		name:        "native_networkconf_id",
		apiKey:      "native_networkconf_id",
		kind:        portFieldString,
		description: "ID of the port's native (untagged) network. The controller uses the default network when unset.",
	},
	{
		name:   "tagged_vlan_mgmt",
		apiKey: "tagged_vlan_mgmt",
		kind:   portFieldString,
		oneOf:  []string{"auto", "block_all", "custom"},
		description: "Which networks the port tags: `auto` tags every network, `block_all` tags none, and " +
			"`custom` tags every network except `excluded_networkconf_ids`. The controller derives the " +
			"legacy `forward` field from this value. A `custom` port that excludes every VLAN network is " +
			"stored by the controller as `block_all`, so declare `block_all` for that case.",
	},
	{
		name:        "excluded_networkconf_ids",
		apiKey:      "excluded_networkconf_ids",
		kind:        portFieldStringSet,
		description: "Networks not tagged on the port when `tagged_vlan_mgmt` is `custom`.",
	},
	{
		name:        "voice_networkconf_id",
		apiKey:      "voice_networkconf_id",
		kind:        portFieldString,
		description: "ID of the port's voice network.",
	},
	{
		name:        "port_profile_id",
		apiKey:      "portconf_id",
		kind:        portFieldString,
		description: "ID of the port profile applied to the port. Settings declared on the port take precedence over the profile.",
	},
	{
		name:   "op_mode",
		apiKey: "op_mode",
		kind:   portFieldString,
		oneOf:  []string{"switch", "mirror", "aggregate"},
		description: "Operating mode. Set `aggregate` on the lead port of a link aggregation group and list its " +
			"ports in `aggregate_members`; the member ports get no entry of their own.",
	},
	{
		name:        "aggregate_members",
		apiKey:      "aggregate_members",
		kind:        portFieldIntSet,
		description: "Port indexes in the link aggregation group led by this port, this port included.",
	},
	{
		name:         "lag_idx",
		apiKey:       "lag_idx",
		kind:         portFieldInt,
		computedOnly: true,
		description:  "Link aggregation group index assigned by the controller.",
	},
	{
		name:        "mirror_port_idx",
		apiKey:      "mirror_port_idx",
		kind:        portFieldInt,
		description: "Index of the port mirrored to this one when `op_mode` is `mirror`.",
	},
	{
		name:        "autoneg",
		apiKey:      "autoneg",
		kind:        portFieldBool,
		description: "Negotiate link speed and duplex automatically.",
	},
	{
		name:        "speed",
		apiKey:      "speed",
		kind:        portFieldInt,
		description: "Link speed in Mbps when `autoneg` is false.",
	},
	{
		name:        "full_duplex",
		apiKey:      "full_duplex",
		kind:        portFieldBool,
		description: "Use full duplex when `autoneg` is false.",
	},
	{
		name:        "poe_mode",
		apiKey:      "poe_mode",
		kind:        portFieldString,
		oneOf:       []string{"auto", "pasv24", "passthrough", "off"},
		description: "PoE mode.",
	},
	{
		name:        "port_keepalive_enabled",
		apiKey:      "port_keepalive_enabled",
		kind:        portFieldBool,
		description: "Enable port keepalive.",
	},
	{
		name:        "stp_port_mode",
		apiKey:      "stp_port_mode",
		kind:        portFieldBool,
		description: "Run spanning tree on the port.",
	},
	{
		name:        "stp_edge_state",
		apiKey:      "stp_edge_state",
		kind:        portFieldString,
		oneOf:       []string{"auto", "enabled", "disabled"},
		description: "Spanning tree edge-port state.",
	},
	{
		name:        "stp_bpdu_guard_enabled",
		apiKey:      "stp_bpdu_guard_enabled",
		kind:        portFieldBool,
		description: "Enable spanning tree BPDU guard.",
	},
	{
		name:        "multicast_router_mode",
		apiKey:      "multicast_router_mode",
		kind:        portFieldString,
		oneOf:       []string{"ALL", "CUSTOM", "NONE"},
		description: "Multicast router mode.",
	},
	{
		name:        "isolation",
		apiKey:      "isolation",
		kind:        portFieldBool,
		description: "Isolate the port from other isolated ports.",
	},
	{
		name:        "port_security",
		kind:        portFieldObject,
		description: "Port security: a MAC allow-list for the port.",
		children: []portField{
			{
				name:        "enabled",
				apiKey:      "port_security_enabled",
				kind:        portFieldBool,
				description: "Restrict the port to `mac_address`.",
			},
			{
				name:        "mac_address",
				apiKey:      "port_security_mac_address",
				kind:        portFieldStringSet,
				description: "MAC addresses allowed when `enabled` is true.",
			},
		},
	},
	{
		name:        "dot1x",
		kind:        portFieldObject,
		description: "802.1X port authentication.",
		children: []portField{
			{
				name:   "ctrl",
				apiKey: "dot1x_ctrl",
				kind:   portFieldString,
				oneOf: []string{
					"auto",
					"force_authorized",
					"force_unauthorized",
					"mac_based",
					"multi_host",
				},
				description: "802.1X control mode.",
			},
			{
				name:        "idle_timeout",
				apiKey:      "dot1x_idle_timeout",
				kind:        portFieldInt,
				description: "802.1X idle timeout in seconds.",
			},
		},
	},
	{
		name:        "egress_rate_limit",
		kind:        portFieldObject,
		description: "Egress rate limiting.",
		children: []portField{
			{
				name:        "enabled",
				apiKey:      "egress_rate_limit_kbps_enabled",
				kind:        portFieldBool,
				description: "Enable egress rate limiting.",
			},
			{
				name:        "kbps",
				apiKey:      "egress_rate_limit_kbps",
				kind:        portFieldInt,
				description: "Egress rate limit in kbps.",
			},
		},
	},
	{
		name:        "lldpmed",
		kind:        portFieldObject,
		description: "LLDP-MED.",
		children: []portField{
			{
				name:        "enabled",
				apiKey:      "lldpmed_enabled",
				kind:        portFieldBool,
				description: "Enable LLDP-MED.",
			},
			{
				name:        "notify_enabled",
				apiKey:      "lldpmed_notify_enabled",
				kind:        portFieldBool,
				description: "Enable LLDP-MED topology change notifications.",
			},
		},
	},
	{
		name:        "stormctrl",
		kind:        portFieldObject,
		description: "Storm control, per traffic class.",
		children: []portField{
			stormctrlClass("bcast", "broadcast"),
			stormctrlClass("mcast", "multicast"),
			stormctrlClass("ucast", "unknown-unicast"),
		},
	},
	{
		name:        "setting_preference",
		apiKey:      "setting_preference",
		kind:        portFieldString,
		oneOf:       []string{"auto", "manual"},
		description: "Whether the port's settings are chosen automatically or set manually.",
	},
	{
		name:        "sd_wan_underlay_port",
		apiKey:      "sd_wan_underlay_port",
		kind:        portFieldBool,
		description: "Use the port as an SD-WAN underlay.",
	},
}

// stormctrlClass is one traffic class of the stormctrl object: its enable flag
// and its rate, keyed stormctrl_<class>_enabled and stormctrl_<class>_rate.
func stormctrlClass(class, label string) portField {
	return portField{
		name:        class,
		kind:        portFieldObject,
		description: "Storm control for " + label + " traffic.",
		children: []portField{
			{
				name:        "enabled",
				apiKey:      "stormctrl_" + class + "_enabled",
				kind:        portFieldBool,
				description: "Enable " + label + " storm control.",
			},
			{
				name:        "rate",
				apiKey:      "stormctrl_" + class + "_rate",
				kind:        portFieldInt,
				description: label + " storm control rate in packets per second.",
			},
		},
	}
}

// portOverrideEntry is one element of a device's port_overrides array, kept as
// raw JSON per key so that keys this provider does not model survive a
// read-modify-write unchanged.
type portOverrideEntry map[string]json.RawMessage

// rawDevice is the part of a device record unifi_device_ports reads.
type rawDevice struct {
	ID            string              `json:"_id"`
	MAC           string              `json:"mac"`
	PortOverrides []portOverrideEntry `json:"port_overrides"`
}

func (e portOverrideEntry) index() (int64, error) {
	raw, ok := e["port_idx"]
	if !ok {
		return 0, fmt.Errorf("port_overrides entry has no port_idx")
	}
	idx, set, err := decodeInt(raw)
	if err != nil || !set {
		return 0, fmt.Errorf("port_overrides entry has an unusable port_idx %s", raw)
	}
	return idx, nil
}

// decodeInt reads a JSON number, or a number inside a JSON string as some
// controllers send them. An empty string or null reads as unset.
func decodeInt(raw json.RawMessage) (v int64, set bool, err error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return 0, false, nil
		}
		v, err = strconv.ParseInt(s, 10, 64)
		return v, err == nil, err
	}
	var n *json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false, err
	}
	if n == nil {
		return 0, false, nil
	}
	v, err = n.Int64()
	return v, err == nil, err
}

// mergePortOverrides builds the port_overrides array to write. declared maps a
// port index to the keys the configuration sets on that port. A declared port
// starts from its live entry, so every key the configuration does not set,
// including keys this provider does not model, is written back unchanged.
// Live entries whose index is not declared are dropped: the resource declares
// the device's complete port configuration.
func mergePortOverrides(
	live []portOverrideEntry,
	declared map[int64]portOverrideEntry,
) ([]portOverrideEntry, error) {
	merged := make([]portOverrideEntry, 0, len(declared))
	seen := make(map[int64]bool, len(declared))
	for _, entry := range live {
		idx, err := entry.index()
		if err != nil {
			return nil, err
		}
		keys, ok := declared[idx]
		if !ok || seen[idx] {
			continue
		}
		seen[idx] = true
		out := make(portOverrideEntry, len(entry)+len(keys))
		for k, v := range entry {
			out[k] = v
		}
		for k, v := range keys {
			out[k] = v
		}
		merged = append(merged, out)
	}

	added := make([]int64, 0, len(declared))
	for idx := range declared {
		if !seen[idx] {
			added = append(added, idx)
		}
	}
	slices.Sort(added)
	for _, idx := range added {
		out := make(portOverrideEntry, len(declared[idx])+1)
		for k, v := range declared[idx] {
			out[k] = v
		}
		out["port_idx"] = json.RawMessage(strconv.FormatInt(idx, 10))
		merged = append(merged, out)
	}
	return merged, nil
}

func portFieldAttrType(f portField) attr.Type {
	switch f.kind {
	case portFieldBool:
		return types.BoolType
	case portFieldInt:
		return types.Int64Type
	case portFieldStringSet:
		return types.SetType{ElemType: types.StringType}
	case portFieldIntSet:
		return types.SetType{ElemType: types.Int64Type}
	case portFieldObject:
		return types.ObjectType{AttrTypes: portFieldsAttrTypes(f.children)}
	default:
		return types.StringType
	}
}

func portFieldsAttrTypes(fields []portField) map[string]attr.Type {
	attrTypes := make(map[string]attr.Type, len(fields))
	for _, f := range fields {
		attrTypes[f.name] = portFieldAttrType(f)
	}
	return attrTypes
}

func devicePortAttrTypes() map[string]attr.Type {
	return portFieldsAttrTypes(portFields)
}

func nullPortValue(f portField) attr.Value {
	switch f.kind {
	case portFieldBool:
		return types.BoolNull()
	case portFieldInt:
		return types.Int64Null()
	case portFieldStringSet:
		return types.SetNull(types.StringType)
	case portFieldIntSet:
		return types.SetNull(types.Int64Type)
	case portFieldObject:
		return types.ObjectNull(portFieldsAttrTypes(f.children))
	default:
		return types.StringNull()
	}
}

func unknownPortValue(f portField) attr.Value {
	switch f.kind {
	case portFieldBool:
		return types.BoolUnknown()
	case portFieldInt:
		return types.Int64Unknown()
	case portFieldStringSet:
		return types.SetUnknown(types.StringType)
	case portFieldIntSet:
		return types.SetUnknown(types.Int64Type)
	case portFieldObject:
		return types.ObjectUnknown(portFieldsAttrTypes(f.children))
	default:
		return types.StringUnknown()
	}
}

// decodePortGroup builds the object of f from the entry: every child is read
// from its own key, and an absent key reads as null. The object is always
// known, so a configuration that declares part of the group plans the rest as
// computed leaves.
func decodePortGroup(f portField, entry portOverrideEntry) (attr.Value, diag.Diagnostics) {
	var diags diag.Diagnostics
	values := make(map[string]attr.Value, len(f.children))
	for _, c := range f.children {
		if c.kind == portFieldObject {
			v, d := decodePortGroup(c, entry)
			diags.Append(d...)
			values[c.name] = v
			continue
		}
		raw, ok := entry[c.apiKey]
		if !ok {
			values[c.name] = nullPortValue(c)
			continue
		}
		v, d := decodePortValue(c, raw)
		diags.Append(d...)
		values[c.name] = v
	}
	obj, d := types.ObjectValue(portFieldsAttrTypes(f.children), values)
	diags.Append(d...)
	return obj, diags
}

// encodeDeclaredFields writes every field of fields that the configuration
// declares (not null in config) and the plan knows into entry, descending into
// object fields. Fields left out of the configuration are not written, so the
// controller keeps their current values.
func encodeDeclaredFields(
	ctx context.Context,
	fields []portField,
	planAttrs, configAttrs map[string]attr.Value,
	entry portOverrideEntry,
) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, f := range fields {
		if f.computedOnly {
			continue
		}
		cv, ok := configAttrs[f.name]
		if !ok || cv.IsNull() {
			continue
		}
		pv := planAttrs[f.name]
		if pv == nil || pv.IsNull() || pv.IsUnknown() {
			continue
		}
		if f.kind == portFieldObject {
			planObj, ok := pv.(types.Object)
			configObj, cok := cv.(types.Object)
			if !ok || !cok || configObj.IsUnknown() {
				continue
			}
			diags.Append(
				encodeDeclaredFields(
					ctx,
					f.children,
					planObj.Attributes(),
					configObj.Attributes(),
					entry,
				)...)
			continue
		}
		raw, d := encodePortValue(ctx, f, pv)
		diags.Append(d...)
		entry[f.apiKey] = raw
	}
	return diags
}

// decodePortValue turns one raw port_overrides value into its Terraform value.
// A JSON null reads as a null attribute.
func decodePortValue(f portField, raw json.RawMessage) (attr.Value, diag.Diagnostics) {
	var diags diag.Diagnostics
	if string(raw) == "null" {
		return nullPortValue(f), diags
	}
	bad := func(err error) (attr.Value, diag.Diagnostics) {
		diags.AddError(
			"Unexpected port_overrides value",
			fmt.Sprintf(
				"Key %q holds %s, which does not decode as the %s attribute: %s",
				f.apiKey,
				raw,
				f.name,
				err,
			),
		)
		return nullPortValue(f), diags
	}
	switch f.kind {
	case portFieldBool:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return bad(err)
		}
		return types.BoolValue(v), diags
	case portFieldInt:
		v, set, err := decodeInt(raw)
		if err != nil {
			return bad(err)
		}
		if !set {
			return types.Int64Null(), diags
		}
		return types.Int64Value(v), diags
	case portFieldStringSet:
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return bad(err)
		}
		elems := make([]attr.Value, 0, len(v))
		for _, s := range v {
			elems = append(elems, types.StringValue(s))
		}
		set, d := types.SetValue(types.StringType, elems)
		diags.Append(d...)
		return set, diags
	case portFieldIntSet:
		var v []json.RawMessage
		if err := json.Unmarshal(raw, &v); err != nil {
			return bad(err)
		}
		elems := make([]attr.Value, 0, len(v))
		for _, r := range v {
			i, set, err := decodeInt(r)
			if err != nil {
				return bad(err)
			}
			if set {
				elems = append(elems, types.Int64Value(i))
			}
		}
		set, d := types.SetValue(types.Int64Type, elems)
		diags.Append(d...)
		return set, diags
	default:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return bad(err)
		}
		return types.StringValue(v), diags
	}
}

// encodePortValue turns a known, non-null Terraform value into its raw
// port_overrides value.
func encodePortValue(
	ctx context.Context,
	f portField,
	v attr.Value,
) (json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics
	var out any
	switch tv := v.(type) {
	case types.Bool:
		out = tv.ValueBool()
	case types.Int64:
		out = tv.ValueInt64()
	case types.String:
		out = tv.ValueString()
	case types.Set:
		if f.kind == portFieldIntSet {
			var s []int64
			diags.Append(tv.ElementsAs(ctx, &s, false)...)
			slices.Sort(s)
			out = s
		} else {
			var s []string
			diags.Append(tv.ElementsAs(ctx, &s, false)...)
			slices.Sort(s)
			out = s
		}
	default:
		diags.AddError(
			"Unable to encode port value",
			fmt.Sprintf("%s: unexpected type %T", f.name, v),
		)
		return nil, diags
	}
	raw, err := json.Marshal(out)
	if err != nil {
		diags.AddError("Unable to encode port value", fmt.Sprintf("%s: %s", f.name, err))
	}
	return raw, diags
}

// portOverridesToState converts the device's port_overrides into the ports map:
// one element per entry, keyed by port index, with every modelled key read from
// the controller and an absent key read as null.
// lagMembers returns the indices of every port that is a member, and not the lead, of a link
// aggregation group some entry declares. The controller stores the group on its lead and ignores
// a member's own entry, and the resource forbids declaring one, so the read leaves them out.
func lagMembers(entries []portOverrideEntry) map[int64]bool {
	members := map[int64]bool{}
	for _, entry := range entries {
		lead, err := entry.index()
		if err != nil {
			continue
		}
		raw, ok := entry["aggregate_members"]
		if !ok {
			continue
		}
		var idxs []json.Number
		if err := json.Unmarshal(raw, &idxs); err != nil {
			continue
		}
		for _, n := range idxs {
			if idx, err := n.Int64(); err == nil && idx != lead {
				members[idx] = true
			}
		}
	}
	return members
}

func portOverridesToState(entries []portOverrideEntry) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	attrTypes := devicePortAttrTypes()
	elemType := types.ObjectType{AttrTypes: attrTypes}
	ports := make(map[string]attr.Value, len(entries))
	members := lagMembers(entries)
	for _, entry := range entries {
		idx, err := entry.index()
		if err != nil {
			diags.AddError("Unexpected port_overrides entry", err.Error())
			continue
		}
		if members[idx] {
			continue
		}
		values := make(map[string]attr.Value, len(portFields))
		for _, f := range portFields {
			if f.kind == portFieldObject {
				v, d := decodePortGroup(f, entry)
				diags.Append(d...)
				values[f.name] = v
				continue
			}
			raw, ok := entry[f.apiKey]
			if !ok {
				values[f.name] = nullPortValue(f)
				continue
			}
			v, d := decodePortValue(f, raw)
			diags.Append(d...)
			values[f.name] = v
		}
		obj, d := types.ObjectValue(attrTypes, values)
		diags.Append(d...)
		ports[strconv.FormatInt(idx, 10)] = obj
	}
	if diags.HasError() {
		return types.MapNull(elemType), diags
	}
	m, d := types.MapValue(elemType, ports)
	diags.Append(d...)
	return m, diags
}

// declaredPorts extracts the keys the configuration sets on each port. config
// decides which attributes are declared (not null); plan supplies their values,
// which are known by apply time. Attributes left out of the configuration are
// not written, so the controller keeps their current values.
func declaredPorts(
	ctx context.Context,
	plan, config types.Map,
) (map[int64]portOverrideEntry, diag.Diagnostics) {
	var diags diag.Diagnostics
	declared := make(map[int64]portOverrideEntry, len(plan.Elements()))
	configElems := config.Elements()
	for key, planElem := range plan.Elements() {
		idx, err := strconv.ParseInt(key, 10, 64)
		if err != nil || idx < 1 {
			diags.AddError(
				"Invalid port index",
				fmt.Sprintf("ports key %q is not a positive port index", key),
			)
			continue
		}
		planObj, ok := planElem.(types.Object)
		if !ok || planObj.IsNull() || planObj.IsUnknown() {
			diags.AddError(
				"Invalid port definition",
				fmt.Sprintf("ports[%q] is not a known object", key),
			)
			continue
		}
		planAttrs := planObj.Attributes()
		var configAttrs map[string]attr.Value
		if c, ok := configElems[key].(types.Object); ok && !c.IsNull() && !c.IsUnknown() {
			configAttrs = c.Attributes()
		}
		entry := portOverrideEntry{}
		diags.Append(encodeDeclaredFields(ctx, portFields, planAttrs, configAttrs, entry)...)
		declared[idx] = entry
	}
	return declared, diags
}
