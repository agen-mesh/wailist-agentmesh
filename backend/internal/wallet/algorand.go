package wallet

type Service struct{}

func NewService(encKey, algodURL, algodToken, network string) *Service { return &Service{} }
func (s *Service) Network() string                                      { return "testnet" }
