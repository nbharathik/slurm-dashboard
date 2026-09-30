package units

import (
	"slices"
	"testing"
)

func TestGPUsByType(t *testing.T) {
	cases := map[string][]GPUCount{
		"gpu:h200:4(S:0-1)":                  {{"h200", 4}},
		"gpu:1g.33gb:4,gpu:1g.16gb:7":        {{"1g.33gb", 4}, {"1g.16gb", 7}},
		"gpu:1g.33gb:1(IDX:0),gpu:1g.16gb:0": {{"1g.33gb", 1}, {"1g.16gb", 0}},
		"gpu:a100:no_consume:4":              {{"a100", 4}},
		"gpu:2,gpu:h100:1,gpu:h100:1":        {{"", 2}, {"h100", 2}},
		"gres/gpu:rtx:1,shard:gpu:4":         {{"rtx", 1}},
		"N/A":                                nil,
		"gpu:x:y":                            nil,
	}
	for in, want := range cases {
		if got := GPUsByType(in); !slices.Equal(got, want) {
			t.Errorf("GPUsByType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGPUsByTypeTRES(t *testing.T) {
	typed, total := GPUsByTypeTRES(ParseTRES("cpu=8,mem=64G,gres/gpu=2,gres/gpu:nvidia_h200_nvl=1,gres/gpu:1g.33gb=1"))
	if total != 2 || !slices.Equal(typed, []GPUCount{{"1g.33gb", 1}, {"nvidia_h200_nvl", 1}}) {
		t.Errorf("got %v %d", typed, total)
	}
	// The colon form some Slurm versions print for MIG.
	typed, total = GPUsByTypeTRES(ParseTRES("cpu=1,gres/gpu:1g.33gb:1"))
	if total != -1 || !slices.Equal(typed, []GPUCount{{"1g.33gb", 1}}) {
		t.Errorf("colon form: %v %d", typed, total)
	}
}

func TestIsMIG(t *testing.T) {
	for _, s := range []string{"1g.33gb", "1g.16gb", "3g.40gb", "1g.10gb+me", "1c.3g.40gb", "nvidia_a100_3g.39gb", "a100-1g.5gb"} {
		if !IsMIG(s) {
			t.Errorf("IsMIG(%q) = false", s)
		}
	}
	for _, s := range []string{"h100_80gb", "a100-sxm4-80gb", "l40s", "nvidia_h200_nvl", ""} {
		if IsMIG(s) {
			t.Errorf("IsMIG(%q) = true", s)
		}
	}
}

func TestGPUDisplayName(t *testing.T) {
	cases := map[string]string{
		"nvidia_h200_nvl": "H200 NVL",
		"nvidia_rtx_pro_6000_blackwell_max-q_workstation_edition": "RTX PRO 6000 BW",
		"nvidia_a100-sxm4-80gb":                                   "A100 80GB",
		"nvidia_a100_80gb_pcie":                                   "A100 80GB",
		"a100":                                                    "A100",
		"nvidia_h100_80gb_hbm3":                                   "H100 80GB",
		"h100":                                                    "H100",
		"nvidia_gh200_480gb":                                      "GH200 480GB",
		"tesla_v100-sxm2-32gb":                                    "V100 32GB",
		"nvidia_l40s":                                             "L40S",
		"nvidia_rtx_a6000":                                        "RTX A6000",
		"nvidia_rtx_6000_ada_generation":                          "RTX 6000 Ada",
		"amd_instinct_mi250x":                                     "MI250X",
		"1g.33gb":                                                 "MIG 1g.33gb",
		"nvidia_a100_3g.39gb":                                     "A100 3g.39gb",
		"tesla":                                                   "TESLA",
		"quadro_rtx_8000_special":                                 "Quadro Rtx 8000 Special",
		"":                                                        "",
	}
	for in, want := range cases {
		if got := GPUDisplayName(in, nil); got != want {
			t.Errorf("GPUDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := GPUDisplayName("NVIDIA_H200_NVL", map[string]string{"nvidia_h200_nvl": "H200"}); got != "H200" {
		t.Errorf("alias: %q", got)
	}
	if got := GPUDisplayNames("h100,1g.33gb", nil); got != "H100, MIG 1g.33gb" {
		t.Errorf("list: %q", got)
	}
}
