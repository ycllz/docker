package winlx

type NtStatus int32

// TODO: Flesh this structure out more.
// Right now has some bare minimum.
type CreateInfo struct {
	Uid   uint32
	Gid   uint32
	DevId uint32
	Time  uint32
}
