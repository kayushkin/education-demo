package server

// Preset is a ready-made lesson so a demo can start in one click without
// waiting on a model call to invent goals.
type Preset struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Subject string   `json:"subject"`
	Goals   []string `json:"goals"`
	Labels  []string `json:"labels"`
}

// Presets are chosen for one property: each has goals with a well-known,
// specific wrong answer a student can state confidently. A goal nobody can be
// confidently wrong about cannot demonstrate the alert that matters most.
var Presets = []Preset{
	{
		ID:      "forces",
		Title:   "Forces and Motion",
		Subject: "Physics · Year 9",
		Goals: []string{
			"Explain why objects of different mass fall at the same rate in a vacuum.",
			"Identify the forces acting on an object moving at constant velocity.",
			"Use Newton's third law to name both forces in an interaction pair.",
			"Distinguish mass from weight, and say which changes on the Moon.",
			"Explain why a moving object does not need a force to keep moving.",
		},
		Labels: []string{"Free fall", "Balanced forces", "Third law", "Mass vs weight", "Inertia"},
	},
	{
		ID:      "fractions",
		Title:   "Adding and Comparing Fractions",
		Subject: "Mathematics · Year 6",
		Goals: []string{
			"Add two fractions with different denominators.",
			"Explain why you cannot add numerators and denominators separately.",
			"Compare two fractions without converting to decimals.",
			"Represent a fraction greater than one as a mixed number.",
			"Explain what stays the same when a fraction is written in an equivalent form.",
		},
		Labels: []string{"Unlike denominators", "Why not add across", "Comparing", "Mixed numbers", "Equivalence"},
	},
	{
		ID:      "photosynthesis",
		Title:   "Photosynthesis and Plant Growth",
		Subject: "Biology · Year 8",
		Goals: []string{
			"State where a plant's mass mostly comes from.",
			"Write the inputs and outputs of photosynthesis.",
			"Explain why plants respire as well as photosynthesise.",
			"Describe what limits the rate of photosynthesis.",
			"Explain the role of chlorophyll without saying it 'makes food'.",
		},
		Labels: []string{"Source of mass", "Inputs/outputs", "Respiration too", "Limiting factors", "Chlorophyll"},
	},
}

func presetByID(id string) *Preset {
	for i := range Presets {
		if Presets[i].ID == id {
			return &Presets[i]
		}
	}
	return nil
}
