export function assertReviewRole(role, review, identity) {
  const rpg = review.rpgMaker;
  const initiallyReady = identity[2] === "ready" || ["rpgxpStandardAmbiguous", "rpgvxStandardAmbiguous"].includes(role);
  if (review.canApprove !== initiallyReady || rpg?.selectedCoreId !== "rpgmaker"
      || rpg.generation !== identity[1] || rpg.runtimePackSelections?.length !==
      (["rpgxpStandardAmbiguous", "rpgvxStandardAmbiguous"].includes(role) ? 1 : 0)
      || rpg.selfContainedOverride) {
    throw new Error(`RPG_009_PROVISION_REVIEW_ROLE_INVALID_${role}`);
  }
  assertReviewRequirements(role, rpg);
}

function assertReviewRequirements(role, rpg) {
  const requirements = rpg.runtimePackRequirements ?? [];
  if (role.endsWith("SelfContained")) {
    if (!rpg.selfContained || requirements.length) { throw new Error("RPG_009_PROVISION_SELF_CONTAINED_INVALID"); }
  } else if (role.endsWith("NoRtp")) {
    if (rpg.selfContained || requirements.length) { throw new Error("RPG_009_PROVISION_NO_RTP_INVALID"); }
  } else if (requirements.length !== 1 || requirements[0].declaredName !== declaredName(role)) {
    throw new Error("RPG_009_PROVISION_REQUIREMENT_INVALID");
  }
}

function declaredName(role) {
  return {
    rpg2000Missing: "RPG2000_RTP", rpg2003Missing: "RPG2003_RTP",
    rpgxpStandardAmbiguous: "Standard", rpgxpCustom: "RetromCustomXP",
    rpgvxStandardAmbiguous: "RPGVX", rpgvxCustom: "RetromCustomVX",
    rpgvxaceStandardAmbiguous: "RPGVXAce", rpgvxaceCustom: "RetromCustomVXAce",
  }[role];
}
