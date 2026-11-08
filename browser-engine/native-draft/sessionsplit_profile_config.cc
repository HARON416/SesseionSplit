// Copyright 2026 The SessionSplit Authors
// Use of this source code is governed by a BSD-style license that can be
// found in the Chromium LICENSE file.

#include "components/embedder_support/sessionsplit_profile_config.h"

#include <algorithm>
#include <utility>

#include "base/i18n/language_tag.h"
#include "base/i18n/tag_converters.h"
#include "base/json/json_reader.h"
#include "base/strings/string_number_conversions.h"
#include "base/values.h"
#include "third_party/icu/source/common/unicode/unistr.h"
#include "third_party/icu/source/i18n/unicode/timezone.h"

namespace embedder_support {

base::expected<SessionSplitProfileConfig, std::string>
ParseSessionSplitProfileConfig(std::string_view json) {
  if (json.empty() || json.size() > SessionSplitProfileConfig::kMaxBytes) {
    return base::unexpected("Profile configuration exceeds the size limit");
  }
  auto dict = base::JSONReader::ReadDict(json, base::JSON_PARSE_RFC, 4);
  if (!dict || dict->size() != 4) {
    return base::unexpected("Profile configuration has invalid fields");
  }
  if (dict->FindInt("schemaVersion") !=
      SessionSplitProfileConfig::kSchemaVersion) {
    return base::unexpected("Unsupported profile configuration schemaVersion");
  }

  const std::string* seed = dict->FindString("seed");
  if (!seed || seed->size() != 64 ||
      !std::ranges::all_of(*seed, [](char c) {
        return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
      })) {
    return base::unexpected("Profile seed must be 32 bytes of lowercase hex");
  }
  std::vector<uint8_t> bytes;
  if (!base::HexStringToBytes(*seed, &bytes) || bytes.size() != 32) {
    return base::unexpected("Invalid profile seed");
  }

  SessionSplitProfileConfig config;
  std::ranges::copy(bytes, config.seed.begin());
  const base::ListValue* languages = dict->FindList("languages");
  if (!languages || languages->empty() || languages->size() > 8) {
    return base::unexpected("Profile languages must contain one to eight tags");
  }
  for (const auto& value : *languages) {
    const std::string* language = value.GetIfString();
    if (!language || language->empty() || language->size() > 64) {
      return base::unexpected("Invalid profile language tag");
    }
    auto tag = base::i18n::GetLanguageTagFromString(*language);
    if (!tag) {
      return base::unexpected("Invalid profile language tag");
    }
    std::string canonical(tag->tag_string());
    if (std::ranges::find(config.languages, canonical) !=
        config.languages.end()) {
      return base::unexpected("Duplicate profile language tag");
    }
    config.languages.push_back(std::move(canonical));
  }

  const std::string* timezone = dict->FindString("timezone");
  if (!timezone || timezone->empty() || timezone->size() > 128 ||
      !std::ranges::all_of(*timezone,
                          [](char c) { return c >= 0x21 && c <= 0x7e; })) {
    return base::unexpected("Invalid profile timezone");
  }
  UErrorCode status = U_ZERO_ERROR;
  UBool is_system_id = false;
  icu::UnicodeString canonical;
  icu::TimeZone::getCanonicalID(icu::UnicodeString::fromUTF8(*timezone),
                              canonical, is_system_id, status);
  if (U_FAILURE(status) || !is_system_id) {
    return base::unexpected("Profile timezone must be a known system zone");
  }
  canonical.toUTF8String(config.timezone);
  return config;
}

}  // namespace embedder_support
