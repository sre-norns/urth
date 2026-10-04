package urth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// AdmissionRejection reports the last policy denial after valid Worker proof.
// It is operator telemetry and grants no Worker authority.
type AdmissionRejection struct {
	Fingerprint string    `json:"fingerprint" yaml:"fingerprint"`
	Reason      string    `json:"reason" yaml:"reason"`
	Time        time.Time `json:"time" yaml:"time"`
}

type admissionPolicyError struct {
	*bark.ErrorResponse
	rejection AdmissionRejection
}

func (e *admissionPolicyError) Unwrap() error { return e.ErrorResponse }

// recordAdmissionRejection runs after the failed enrollment transaction rolls
// back. It changes only operational status, not policy or resource version.
func (m *runnersAPIImpl) recordAdmissionRejection(ctx context.Context, id manifest.ResourceID, err error) {
	var denial *admissionPolicyError
	if !errors.As(err, &denial) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	encoded, encodeErr := json.Marshal(denial.rejection)
	if encodeErr != nil {
		return
	}
	if updateErr := m.workerDB.WithContext(ctx).Model(&Runner{}).Where("uid = ?", id).UpdateColumn("status_last_admission_rejection", string(encoded)).Error; updateErr != nil {
		log.Printf("cannot record Worker admission denial: %v", updateErr)
	}
}
