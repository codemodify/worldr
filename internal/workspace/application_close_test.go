package workspace

type closingApplications struct {
	*fakeApplications
	closed []uint64
	check  func(uint64)
}

func (f *closingApplications) CloseApplication(id uint64) {
	if f.check != nil {
		f.check(id)
	}
	f.closed = append(f.closed, id)
}
