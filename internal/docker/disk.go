package docker

import "time"

// DiskGroup is the disk space one kind of thing uses.
type DiskGroup struct {
	// Size is the space it uses, in bytes.
	Size int64
	// Count is how many there are, and Active how many are in use.
	Count, Active int64
	// Reclaimable is the space that removing what isn't in use frees. It's
	// zero for kinds the daemon doesn't say it for.
	Reclaimable int64
}

// DiskUsage is the disk space the daemon uses.
type DiskUsage struct {
	Images, Containers, Volumes, BuildCache DiskGroup
	// Total is all of it, in bytes.
	Total int64
	// At is when the daemon worked it out.
	At time.Time
	// Volume is the size of each volume the daemon measured, by name.
	Volume map[string]VolumeUse
}

// VolumeUse is how much of the disk a volume uses.
type VolumeUse struct {
	Size int64
	// Refs is how many containers use it.
	Refs int64
}

// parseDf reads the disk usage. Newer daemons add totals next to the lists of
// each kind, and older ones have only the lists, so it reads either.
func parseDf(df *RawDf, at time.Time) *DiskUsage {
	out := &DiskUsage{At: at, Volume: map[string]VolumeUse{}}
	for _, v := range df.Volumes {
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			out.Volume[v.Name] = VolumeUse{Size: v.UsageData.Size, Refs: v.UsageData.RefCount}
		}
	}

	img := &out.Images
	if df.Images != nil {
		img.Count = int64(len(df.Images))
		for _, i := range df.Images {
			img.Size += max(i.Size, 0)
			if i.Containers > 0 {
				img.Active++
			} else {
				img.Reclaimable += max(i.Size-max(i.SharedSize, 0), 0)
			}
		}
	} else if u := df.ImageUsage; u != nil {
		*img = DiskGroup{Size: u.TotalSize, Count: u.TotalCount, Active: u.ActiveCount, Reclaimable: u.Reclaimable}
	}
	// The layers are counted once, however many images share them.
	if df.LayersSize != nil {
		img.Size = *df.LayersSize
	}

	ctr := &out.Containers
	if df.Containers != nil {
		ctr.Count = int64(len(df.Containers))
		for _, c := range df.Containers {
			ctr.Size += max(c.SizeRw, 0)
			if c.State == "running" {
				ctr.Active++
			}
		}
	} else if u := df.ContainerUsage; u != nil {
		*ctr = DiskGroup{Size: u.TotalSize, Count: u.TotalCount, Active: u.ActiveCount}
	}

	vol := &out.Volumes
	if df.Volumes != nil {
		vol.Count = int64(len(df.Volumes))
		for _, v := range out.Volume {
			vol.Size += v.Size
			if v.Refs > 0 {
				vol.Active++
			} else {
				vol.Reclaimable += v.Size
			}
		}
	} else if u := df.VolumeUsage; u != nil {
		*vol = DiskGroup{Size: u.TotalSize, Count: u.TotalCount, Active: u.ActiveCount, Reclaimable: u.Reclaimable}
	}

	bc := &out.BuildCache
	if df.BuildCache != nil {
		bc.Count = int64(len(df.BuildCache))
		for _, b := range df.BuildCache {
			bc.Size += max(b.Size, 0)
		}
	} else if u := df.BuildCacheUsage; u != nil {
		bc.Size, bc.Count = u.TotalSize, u.TotalCount
	}

	out.Total = img.Size + ctr.Size + vol.Size + bc.Size
	return out
}

// VolumeUse returns how much of the disk the volume with a name uses, if the
// daemon measured it. It is safe to call on nil.
func (d *DiskUsage) VolumeUse(name string) (VolumeUse, bool) {
	if d == nil {
		return VolumeUse{}, false
	}
	u, ok := d.Volume[name]
	return u, ok
}
