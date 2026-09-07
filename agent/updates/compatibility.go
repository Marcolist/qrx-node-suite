package updates

import (
	"fmt"

	"qrx-node-suite/agent/version"
)

// CheckAdapterCompatibility looks up the compatibility matrix for an
// adapter component update BEFORE activation, per
// docs/updates.md#breaking-update-protection. This is a manifest-time
// gate, distinct from (and in addition to) components.Adapter.HealthCheck's
// post-restart runtime probe: this one can reject an update without ever
// downloading or restarting anything, the moment the target version is
// known not to work with the currently detected QRX Core version.
func CheckAdapterCompatibility(matrix *version.Matrix, qrxCoreVersion, adapterName, targetAdapterVersion string) error {
	if qrxCoreVersion == "" {
		return nil // QRX Core version not yet detected -- nothing to check against
	}
	_, status := matrix.Lookup(qrxCoreVersion, adapterName, targetAdapterVersion)
	switch status {
	case version.Supported, version.Partial, version.Experimental:
		return nil
	default:
		return fmt.Errorf("updates: %s v%s is %s for QRX Core %s per the compatibility matrix -- BLOCK UPDATE",
			adapterName, targetAdapterVersion, status, qrxCoreVersion)
	}
}
