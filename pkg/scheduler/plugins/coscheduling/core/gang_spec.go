package core

import (
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"

	"github.com/koordinator-sh/koordinator/apis/extension"
)

type GangSpec struct {
	Mode        string
	MinMember   int
	TotalMember int
	Groups      []string
	WaitTime    time.Duration
	GangFrom    string
	ParseError  error
}

func NewGangSpec(obj metav1.Object, defaultWaitTime time.Duration) GangSpec {
	var spec GangSpec
	switch t := obj.(type) {
	case *corev1.Pod:
		spec = NewGangSpecFromPod(t, defaultWaitTime)
	case *v1alpha1.PodGroup:
		spec = NewGangSpecFromPodGroup(t, defaultWaitTime)
	default:
		spec = GangSpec{
			ParseError: fmt.Errorf("unsupported Gang object %v", GetNamespacedName(obj)),
		}
	}
	return spec
}

func NewGangSpecFromPod(pod *corev1.Pod, defaultWaitTime time.Duration) GangSpec {
	var parseErrs []error
	minMember, err := extension.GetMinNum(pod)
	if err != nil {
		parseErrs = append(parseErrs, fmt.Errorf("minMemeber: %w", err))
	}

	totalMember := minMember
	if s := pod.Annotations[extension.AnnotationGangTotalNum]; s != "" {
		val, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("totalMember: %w", err))
		} else if val != 0 && val > int64(minMember) {
			totalMember = int(val)
		}
	}

	mode := pod.Annotations[extension.AnnotationGangMode]
	if mode != extension.GangModeStrict && mode != extension.GangModeNonStrict {
		mode = extension.GangModeStrict
	}

	groups, err := ParseGangGroups(pod.Annotations[extension.AnnotationGangGroups])
	if err != nil {
		parseErrs = append(parseErrs, fmt.Errorf("gangGroups: %w", err))
	}
	if len(groups) == 0 {
		groups = append(groups, GetGangFullName(pod))
	}

	waitTime := defaultWaitTime
	if s := pod.Annotations[extension.AnnotationGangWaitTime]; s != "" {
		val, err := time.ParseDuration(s)
		if err == nil && val > 0 {
			waitTime = val
		}
	}

	return GangSpec{
		Mode:        mode,
		MinMember:   minMember,
		TotalMember: totalMember,
		Groups:      groups,
		WaitTime:    waitTime,
		GangFrom:    GangFromPodAnnotation,
		ParseError:  utilerrors.NewAggregate(parseErrs),
	}
}

func NewGangSpecFromPodGroup(pg *v1alpha1.PodGroup, defaultWaitTime time.Duration) GangSpec {
	minMember := int(pg.Spec.MinMember)

	var parseErrs []error
	totalMember := minMember
	if s := pg.Annotations[extension.AnnotationGangTotalNum]; s != "" {
		val, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("totalMember: %w", err))
		} else if val != 0 && val > int64(minMember) {
			totalMember = int(val)
		}
	}

	mode := pg.Annotations[extension.AnnotationGangMode]
	if mode != extension.GangModeStrict && mode != extension.GangModeNonStrict {
		mode = extension.GangModeStrict
	}

	waitTime := defaultWaitTime
	if pg.Spec.ScheduleTimeoutSeconds != nil {
		duration, err := ParseGangTimeoutSeconds(*pg.Spec.ScheduleTimeoutSeconds)
		if err == nil && duration > 0 {
			waitTime = duration
		}
	}

	groups, err := ParseGangGroups(pg.Annotations[extension.AnnotationGangGroups])
	if err != nil {
		parseErrs = append(parseErrs, fmt.Errorf("groups: %w", err))
	}
	if len(groups) == 0 {
		groups = append(groups, GetNamespacedName(pg))
	}
	return GangSpec{
		Mode:        mode,
		MinMember:   minMember,
		TotalMember: totalMember,
		Groups:      groups,
		WaitTime:    waitTime,
		GangFrom:    GangFromPodGroup,
		ParseError:  utilerrors.NewAggregate(parseErrs),
	}
}
