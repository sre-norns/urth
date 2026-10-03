package natsq

import (
	"fmt"
	"github.com/nats-io/jwt/v2"
)

// ServicePermissions is the minimum subject contract for the three implemented
// broker roles. Operators provision separate JWT/NKeys with these permissions.
func ServicePermissions(role string) (jwt.Permissions, error) {
	p := jwt.Permissions{Sub: jwt.Permission{Allow: jwt.StringList{"_INBOX.>"}}}
	switch role {
	case "provisioner":
		p.Pub.Allow = jwt.StringList{
			"$JS.API.STREAM.CREATE." + JobsStreamName, "$JS.API.STREAM.UPDATE." + JobsStreamName, "$JS.API.STREAM.INFO." + JobsStreamName,
			"$JS.API.STREAM.MSG.DELETE." + JobsStreamName, "$JS.API.STREAM.MSG.GET." + JobsStreamName,
			"$JS.API.CONSUMER.CREATE." + JobsStreamName + ".>", "$JS.API.CONSUMER.INFO." + JobsStreamName + ".>", "$JS.API.CONSUMER.DELETE." + JobsStreamName + ".>",
		}
	case "publisher":
		p.Pub.Allow = jwt.StringList{JobsSubjectWildcard}
	case "observer":
		p.Pub.Deny = jwt.StringList{">"}
		p.Sub.Allow = jwt.StringList{SubjectPrefix + ".logs.*.*", AllPresenceSubjects, MaxDeliveryAdvisorySubject}
	default:
		return jwt.Permissions{}, fmt.Errorf("unknown NATS service role")
	}
	return p, nil
}
