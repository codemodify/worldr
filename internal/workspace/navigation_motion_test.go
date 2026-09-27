package workspace

import (
	"math"
	"testing"
	"time"
)

func navigationMotionFixture() (start, target [MaxApplicationLayouts]navigationPose) {
	for i := range start {
		start[i] = navigationPose{bounds: box{x: float32(i) * 30, y: 80, w: 12, h: 8}}
		target[i] = start[i]
	}
	start[0] = navigationPose{bounds: box{40, 100, 240, 160}, visible: true}
	target[0] = navigationPose{bounds: box{120, 40, 1040, 720}, visible: true}
	return start, target
}

func TestNavigationMotionInitialLayoutAndExactEndpoints(t *testing.T) {
	start, target := navigationMotionFixture()
	var motion navigationMotion
	if motion.moving() {
		t.Fatal("uninitialized motion should be stationary")
	}
	motion.update(time.Second)
	motion.retarget(start)
	if motion.poses() != start || motion.moving() {
		t.Fatal("initial layout should be presented without an entrance animation")
	}
	motion.retarget(target)
	if motion.poses() != start || !motion.moving() {
		t.Fatal("retarget jumped away from the current presentation")
	}
	motion.update(navigationMotionDuration / 2)
	if got := motion.poses()[0]; got != (navigationPose{bounds: box{80, 70, 640, 440}, visible: true}) {
		t.Fatalf("incorrect transition midpoint: %+v", got)
	}
	if !motion.moving() {
		t.Fatal("midpoint incorrectly ended the transition")
	}
	motion.update(navigationMotionDuration / 2)
	if motion.poses() != target || motion.moving() {
		t.Fatal("transition did not finish on the exact target")
	}
	motion.update(time.Second)
	if motion.poses() != target {
		t.Fatal("completed transition continued moving")
	}
	owned := motion.poses()
	owned[0].bounds.x++
	if motion.poses() != target {
		t.Fatal("poses exposed mutable motion storage")
	}
}

func TestNavigationMotionInterruptionAndReversalStartAtPresentation(t *testing.T) {
	start, target := navigationMotionFixture()
	var motion navigationMotion
	motion.snap(start)
	motion.retarget(target)
	motion.update(navigationMotionDuration / 4)
	interrupted := motion.poses()
	if interrupted == start || interrupted == target {
		t.Fatal("test requires an intermediate presentation")
	}
	motion.retarget(start)
	if motion.poses() != interrupted || !motion.moving() {
		t.Fatal("reversal jumped to an earlier endpoint")
	}
	motion.update(navigationMotionDuration / 2)
	got := motion.poses()[0].bounds
	a, b := interrupted[0].bounds, start[0].bounds
	if want := (box{a.x + (b.x-a.x)*.5, a.y + (b.y-a.y)*.5, a.w + (b.w-a.w)*.5, a.h + (b.h-a.h)*.5}); got != want {
		t.Fatalf("reversal interpolated from stale layout: got %+v, want %+v", got, want)
	}
	// Interrupt again with a third destination, retaining the current pose.
	third := target
	third[0].bounds = box{700, 150, 180, 120}
	interrupted = motion.poses()
	motion.retarget(third)
	if motion.poses() != interrupted {
		t.Fatal("second retarget jumped")
	}
	motion.update(navigationMotionDuration)
	if motion.poses() != third || motion.moving() {
		t.Fatal("interrupted motion did not finish at the latest destination")
	}
}

func TestNavigationMotionUnchangedTargetsDoNotRestart(t *testing.T) {
	start, target := navigationMotionFixture()
	var motion navigationMotion
	motion.snap(start)
	motion.retarget(target)
	for range 13 {
		elapsed, presented := motion.elapsed, motion.poses()
		motion.retarget(target)
		if motion.elapsed != elapsed || motion.poses() != presented {
			t.Fatal("repeated layout restarted or altered the transition")
		}
		motion.update(20 * time.Millisecond)
	}
	if motion.moving() || motion.poses() != target {
		t.Fatal("continuous layout updates prevented the transition from finishing")
	}
	motion.retarget(target)
	if motion.moving() {
		t.Fatal("unchanged completed layout started another animation")
	}
}

func TestNavigationMotionCadenceAndInvalidDelta(t *testing.T) {
	start, target := navigationMotionFixture()
	var one, many navigationMotion
	one.snap(start)
	many.snap(start)
	one.retarget(target)
	many.retarget(target)
	one.update(130 * time.Millisecond)
	for _, delta := range []time.Duration{7, 31, 15, 47, 30} {
		many.update(delta * time.Millisecond)
	}
	if one.poses() != many.poses() || one.elapsed != many.elapsed {
		t.Fatal("presentation depended on update cadence")
	}
	before, elapsed := many.poses(), many.elapsed
	for _, delta := range []time.Duration{0, -time.Millisecond, time.Duration(math.MinInt64)} {
		many.update(delta)
		if many.poses() != before || many.elapsed != elapsed {
			t.Fatal("nonpositive elapsed time changed the presentation")
		}
	}
	many.update(time.Duration(math.MaxInt64))
	if many.poses() != target || many.moving() {
		t.Fatal("large elapsed time overflowed or failed to finish")
	}
}

func TestNavigationMotionVisibilityAndCollapseDestinations(t *testing.T) {
	start, target := navigationMotionFixture()
	start[0] = navigationPose{bounds: box{40, 100, 240, 160}, visible: true}
	target[0] = navigationPose{bounds: box{400, 600, 12, 8}}
	start[1] = navigationPose{bounds: box{700, 600, 12, 8}}
	target[1] = navigationPose{bounds: box{120, 40, 1040, 720}, visible: true}
	start[2] = navigationPose{bounds: box{700, 600, 12, 8}}
	target[2] = navigationPose{bounds: box{10, 10, 6, 4}}
	var motion navigationMotion
	motion.snap(start)
	motion.retarget(target)
	if p := motion.poses(); !p[0].visible || !p[1].visible || p[2].visible || p[0].bounds != start[0].bounds || p[1].bounds != start[1].bounds {
		t.Fatal("transition visibility did not preserve either visible endpoint")
	}
	motion.update(navigationMotionDuration / 2)
	p := motion.poses()
	if p[0].bounds != (box{220, 350, 126, 84}) || p[1].bounds != (box{410, 320, 526, 364}) || !p[0].visible || !p[1].visible || p[2].visible {
		t.Fatalf("hidden transition ignored its supplied positive collapse bounds: %+v", p[:3])
	}
	motion.update(navigationMotionDuration / 2)
	if motion.poses() != target {
		t.Fatal("hidden endpoint stayed visible after completion")
	}
	// Reversing a collapse midway expands the currently visible presentation.
	motion.snap(start)
	motion.retarget(target)
	motion.update(100 * time.Millisecond)
	before := motion.poses()
	motion.retarget(start)
	if motion.poses() != before {
		t.Fatal("reversing visibility reset a collapse instead of continuing it")
	}
	motion.update(navigationMotionDuration)
	if motion.poses() != start {
		t.Fatal("reversed visibility did not restore the original endpoints")
	}
}

func TestNavigationMotionKeepsSlotsAndSnapCancelsTransition(t *testing.T) {
	start, target := navigationMotionFixture()
	start[31] = navigationPose{bounds: box{1000, 200, 200, 100}, visible: true}
	target[0].bounds = box{1100, 300, 220, 150}
	target[31] = navigationPose{bounds: box{50, 400, 300, 180}, visible: true}
	var motion navigationMotion
	motion.snap(start)
	motion.retarget(target)
	motion.update(navigationMotionDuration / 2)
	p := motion.poses()
	if p[0].bounds.x != 570 || p[31].bounds.x != 525 || p[1] != start[1] {
		t.Fatal("crossing positions reordered slot identity or moved an unchanged slot")
	}
	motion.snap(target)
	if motion.moving() || motion.poses() != target {
		t.Fatal("reduced-motion snap did not immediately apply the target")
	}
	motion.update(time.Second)
	motion.retarget(target)
	if motion.poses() != target || motion.moving() {
		t.Fatal("snap left a stale transition active")
	}
}
