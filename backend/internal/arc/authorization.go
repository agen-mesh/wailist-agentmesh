package arc

// AuthorizationData encodes a previously verified customer authorization.
// Dispatch and signing remain the responsibility of the durable purchase journal.
func AuthorizationData(a Authorization) ([]byte, error) { return packTransferWithAuthorization(a) }
