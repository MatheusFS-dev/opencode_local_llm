package main

import "testing"

func TestFormatGPUStatusUsesReadableUtilizationAndVRAM(t *testing.T) {
	got := formatGPUStatus("NVIDIA GeForce RTX 3060, 0, 763, 12288\nNVIDIA GeForce RTX 4070, 5, 12282, 12282")
	want := "GPU: NVIDIA GeForce RTX 3060\n0% utilization, 763 MiB / 12,288 MiB VRAM used\n\nGPU: NVIDIA GeForce RTX 4070\n5% utilization, 12,282 MiB / 12,282 MiB VRAM used"
	if got != want {
		t.Fatalf("formatGPUStatus() = %q, want %q", got, want)
	}
}

func TestFormatGPUStatusRejectsMalformedRows(t *testing.T) {
	if got := formatGPUStatus("NVIDIA GPU, unavailable"); got != "Unavailable" {
		t.Fatalf("formatGPUStatus() = %q, want Unavailable", got)
	}
}
