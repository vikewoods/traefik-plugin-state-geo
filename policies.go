package traefik_plugin_state_geo

import (
	"fmt"
	"strings"
)

type decisionPolicy uint8

const (
	decisionPolicyUnknown decisionPolicy = iota
	decisionPolicyAllow
	decisionPolicyDeny
)

type databaseFailurePolicy uint8

const (
	databaseFailurePolicyUnknown databaseFailurePolicy = iota
	databaseFailurePolicyAllow
	databaseFailurePolicyDeny
	databaseFailurePolicyError
)

type privateIPPolicy uint8

const (
	privateIPPolicyUnknown privateIPPolicy = iota
	privateIPPolicyAllow
	privateIPPolicyLookup
	privateIPPolicyDeny
)

type policySet struct {
	databaseFailure    databaseFailurePolicy
	lookupFailure      decisionPolicy
	invalidClientIP    decisionPolicy
	unknownCountry     decisionPolicy
	unknownSubdivision decisionPolicy
	privateIP          privateIPPolicy
}

func parsePolicySet(config *Config) (policySet, error) {
	databaseFailure, err := parseDatabaseFailurePolicy(config.DatabaseFailurePolicy, config.FailOpen)
	if err != nil {
		return policySet{}, err
	}

	lookupFailure, err := parseDecisionPolicy(
		"lookupFailurePolicy",
		config.LookupFailurePolicy,
		decisionPolicyAllow,
	)
	if err != nil {
		return policySet{}, err
	}

	invalidClientIP, err := parseDecisionPolicy(
		"invalidClientIPPolicy",
		config.InvalidClientIPPolicy,
		decisionPolicyDeny,
	)
	if err != nil {
		return policySet{}, err
	}

	unknownCountry, err := parseDecisionPolicy(
		"unknownCountryPolicy",
		config.UnknownCountryPolicy,
		decisionPolicyAllow,
	)
	if err != nil {
		return policySet{}, err
	}

	unknownSubdivision, err := parseDecisionPolicy(
		"unknownSubdivisionPolicy",
		config.UnknownSubdivisionPolicy,
		decisionPolicyDeny,
	)
	if err != nil {
		return policySet{}, err
	}

	privateIP, err := parsePrivateIPPolicy(config.PrivateIPPolicy)
	if err != nil {
		return policySet{}, err
	}

	return policySet{
		databaseFailure:    databaseFailure,
		lookupFailure:      lookupFailure,
		invalidClientIP:    invalidClientIP,
		unknownCountry:     unknownCountry,
		unknownSubdivision: unknownSubdivision,
		privateIP:          privateIP,
	}, nil
}

func parseDatabaseFailurePolicy(rawPolicy string, failOpen bool) (databaseFailurePolicy, error) {
	policy := strings.ToLower(strings.TrimSpace(rawPolicy))
	switch policy {
	case "", "legacy":
		if failOpen {
			return databaseFailurePolicyAllow, nil
		}
		return databaseFailurePolicyError, nil
	case "allow":
		return databaseFailurePolicyAllow, nil
	case "deny":
		return databaseFailurePolicyDeny, nil
	case "error":
		return databaseFailurePolicyError, nil
	default:
		return databaseFailurePolicyUnknown, fmt.Errorf(
			"invalid databaseFailurePolicy %q: expected allow, deny, error, or legacy",
			rawPolicy,
		)
	}
}

func parseDecisionPolicy(fieldName, rawPolicy string, defaultPolicy decisionPolicy) (decisionPolicy, error) {
	policy := strings.ToLower(strings.TrimSpace(rawPolicy))
	switch policy {
	case "":
		return defaultPolicy, nil
	case "allow":
		return decisionPolicyAllow, nil
	case "deny":
		return decisionPolicyDeny, nil
	default:
		return decisionPolicyUnknown, fmt.Errorf(
			"invalid %s %q: expected allow or deny",
			fieldName,
			rawPolicy,
		)
	}
}

func parsePrivateIPPolicy(rawPolicy string) (privateIPPolicy, error) {
	policy := strings.ToLower(strings.TrimSpace(rawPolicy))
	switch policy {
	case "allow":
		return privateIPPolicyAllow, nil
	case "lookup":
		return privateIPPolicyLookup, nil
	case "", "deny":
		return privateIPPolicyDeny, nil
	default:
		return privateIPPolicyUnknown, fmt.Errorf(
			"invalid privateIPPolicy %q: expected allow, lookup, or deny",
			rawPolicy,
		)
	}
}

func databaseFailurePolicyName(policy databaseFailurePolicy) string {
	switch policy {
	case databaseFailurePolicyAllow:
		return "allow"
	case databaseFailurePolicyDeny:
		return "deny"
	case databaseFailurePolicyError:
		return "error"
	default:
		return "unknown"
	}
}
