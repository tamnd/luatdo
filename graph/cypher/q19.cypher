// Question 19. Two provisions regulate the same concept and impose incompatible
// modalities on the same actor for the same action. Find them.
//
// This reads the detector's findings rather than recomputing them. The earlier
// version of this file compared bearer and action as the drafters wrote them and
// said in its own comment that the rows were candidates and not findings,
// because two norms agreeing on bearer and action are usually told apart by a
// condition the comparison ignored. Deciding a pair needs canonical wording,
// intersected validity intervals, condition containment and honoured deferrals,
// none of which is a thing to write in Cypher, so it is done in Go by luatdo
// conflicts check and projected here.
//
// Read circumstances before believing a row. Shared means one norm's conditions
// contain the other's, so every case the narrower one reaches the wider one
// reaches too and the clash can really happen. Unknown means neither contains
// the other and the two may never be triggered together, which is a fact about
// what this comparison can see rather than about the law.
MATCH (k:Conflict)-[:INVOLVES {side: 'a'}]->(a:Norm)
MATCH (k)-[:INVOLVES {side: 'b'}]->(b:Norm)
MATCH (pa:Component)-[:HAS_NORM]->(a)
MATCH (pb:Component)-[:HAS_NORM]->(b)
WHERE $rule IS NULL OR k.rule = $rule
// The concepts come back beside the finding and take no part in deciding it.
// The old query paired norms on a shared concept, which is why it needed the
// concept to exist, and the detector pairs on a canonical party and act
// instead, so a pair that clashes over wording no concept was ever attached to
// is still reported. What the two norms are both about is worth reading, and
// two norms can be about several things at once, so it is a list.
OPTIONAL MATCH (a)-[:ABOUT_CONCEPT]->(c:Concept)<-[:ABOUT_CONCEPT]-(b)
WITH k, a, b, pa, pb, c ORDER BY c.label_vi
WITH k, a, b, pa, pb, collect(DISTINCT c.label_vi) AS concepts
RETURN k.rule AS rule,
       k.circumstances AS circumstances,
       concepts,
       k.party AS actor,
       k.act AS action,
       k.matched AS matched,
       k.clashing_slot AS differs_in, k.clashing_a AS a_says, k.clashing_b AS b_says,
       pa.id AS provision_a, a.norm_type AS modality_a, a.evidence_quote AS quote_a,
       pb.id AS provision_b, b.norm_type AS modality_b, b.evidence_quote AS quote_b,
       k.rank AS ranking,
       k.explanation AS explanation
ORDER BY circumstances, rule, provision_a, provision_b
LIMIT $limit
