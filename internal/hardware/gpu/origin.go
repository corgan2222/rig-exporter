package gpu

// ADLXOrigin is how the AMD driver names itself in gpu_source and in the
// origin of every reading it supplies.
//
// It is exported, and deliberately not a literal repeated elsewhere, because
// the interface has to tell three situations apart: no AMD card, an AMD card
// whose driver is answering, and an AMD card whose driver is silent. The last
// one is the only case where pointing at the AMD download helps, and a renamed
// label would otherwise turn that hint off without anything failing.
//
// This file carries no build tag so the constant is also available where the
// Windows-only collection code is not.
const ADLXOrigin = "AMD ADLX"
