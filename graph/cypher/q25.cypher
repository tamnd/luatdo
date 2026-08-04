// Question 25. Which acts does the corpus attach a penalty to, and which
// provision attaches it?
//
// The join is through the norm and never through the act label. Both norms on
// article 94 are about paying wages and only one of them states a fine, so a
// query that matched the fined act against every act with the same label would
// report a penalty on the prohibition beside it that no provision imposes. The
// slot property is what keeps the two apart: the act the statement is about
// arrives on the action slot, and the penalty on the sanction slot of the same
// statement.
MATCH (n:Norm)-[:ABOUT_ACT {slot: "action"}]->(a:Act)
MATCH (n)-[p:ABOUT_ACT {slot: "sanction"}]->(penalty:Act)
OPTIONAL MATCH (n)-[:HAS_SANCTION]->(s:Sanction)
RETURN a.label_vi AS act,
       penalty.label_vi AS penalty,
       n.norm_type AS norm_type,
       n.modality AS modality,
       p.provision_id AS stated_in,
       s.text AS sanction_text,
       s.legal_basis AS legal_basis
ORDER BY act, penalty
LIMIT $limit
