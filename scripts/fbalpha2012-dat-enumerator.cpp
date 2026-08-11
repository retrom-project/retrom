#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iomanip>
#include <iostream>
#include <map>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

struct BurnRomInfo {
  char name[100];
  std::uint32_t length;
  std::uint32_t crc;
  std::uint32_t type;
};

extern "C" {
int BurnLibInit();
int BurnLibExit();
char* BurnDrvGetTextA(std::uint32_t field);
int BurnDrvGetRomInfo(BurnRomInfo* info, std::uint32_t ordinal);
int BurnDrvGetRomName(char** name, std::uint32_t ordinal, int alias);
int BurnDrvGetFlags();
extern std::uint32_t nBurnDrvCount;
extern std::uint32_t nBurnDrvActive;
}

namespace {

constexpr std::uint32_t kDriverName = 0;
constexpr std::uint32_t kDriverDate = 1;
constexpr std::uint32_t kDriverFullName = 2;
constexpr std::uint32_t kDriverManufacturer = 5;
constexpr std::uint32_t kDriverParent = 7;
constexpr std::uint32_t kDriverBoardROM = 8;
constexpr int kBoardROMFlag = 1 << 3;

struct ROM {
  std::string name;
  std::uint32_t size;
  std::uint32_t crc;
};

struct Machine {
  std::string name;
  std::string description;
  std::string year;
  std::string manufacturer;
  std::string parent;
  std::string boardROM;
  bool boardROMMachine;
  std::vector<ROM> roms;
};

std::string text(std::uint32_t field) {
  const char* value = BurnDrvGetTextA(field);
  return value == nullptr ? std::string{} : std::string(value);
}

std::string xmlEscape(std::string_view value) {
  std::string escaped;
  escaped.reserve(value.size());
  for (const unsigned char character : value) {
    if (character < 0x20 && character != '\t' && character != '\n' && character != '\r') {
      throw std::runtime_error("FBA2012_DAT_INVALID_XML_CHARACTER");
    }
    switch (character) {
      case '&':
        escaped += "&amp;";
        break;
      case '<':
        escaped += "&lt;";
        break;
      case '>':
        escaped += "&gt;";
        break;
      case '"':
        escaped += "&quot;";
        break;
      case '\'':
        escaped += "&apos;";
        break;
      default:
        escaped.push_back(static_cast<char>(character));
    }
  }
  return escaped;
}

std::string crc32(std::uint32_t value) {
  std::ostringstream output;
  output << std::hex << std::nouppercase << std::setw(8) << std::setfill('0') << value;
  return output.str();
}

std::vector<Machine> enumerateMachines() {
  if (BurnLibInit() != 0) {
    throw std::runtime_error("FBA2012_DAT_BURN_INIT_FAILED");
  }
  std::vector<Machine> machines;
  machines.reserve(nBurnDrvCount);
  for (nBurnDrvActive = 0; nBurnDrvActive < nBurnDrvCount; ++nBurnDrvActive) {
    Machine machine{
        .name = text(kDriverName),
        .description = text(kDriverFullName),
        .year = text(kDriverDate),
        .manufacturer = text(kDriverManufacturer),
        .parent = text(kDriverParent),
        .boardROM = text(kDriverBoardROM),
        .boardROMMachine = (BurnDrvGetFlags() & kBoardROMFlag) != 0,
        .roms = {},
    };
    if (machine.name.empty()) {
      throw std::runtime_error("FBA2012_DAT_EMPTY_MACHINE_NAME");
    }
    for (std::uint32_t ordinal = 0;; ++ordinal) {
      BurnRomInfo info{};
      if (BurnDrvGetRomInfo(&info, ordinal) != 0) {
        break;
      }
      char* declaredName = nullptr;
      if (BurnDrvGetRomName(&declaredName, ordinal, 0) != 0 || declaredName == nullptr || *declaredName == '\0') {
        throw std::runtime_error("FBA2012_DAT_EMPTY_ROM_NAME");
      }
      machine.roms.push_back(ROM{
          .name = declaredName,
          .size = info.length,
          .crc = info.crc,
      });
    }
    machines.push_back(std::move(machine));
  }
  BurnLibExit();
  return machines;
}

using ROMIndex = std::map<std::string, std::vector<const ROM*>>;

ROMIndex indexROMs(const Machine& machine) {
  ROMIndex index;
  for (const ROM& rom : machine.roms) {
    index[rom.name].push_back(&rom);
  }
  return index;
}

bool matchesParent(const ROM& rom, const ROMIndex& parent) {
  const auto entries = parent.find(rom.name);
  if (entries == parent.end()) {
    return false;
  }
  return std::any_of(entries->second.begin(), entries->second.end(), [&rom](const ROM* candidate) {
    return rom.size == candidate->size && rom.crc == candidate->crc;
  });
}

void writeMachine(
    const Machine& machine,
    const std::string& parent,
    const std::string& romOf,
    bool isBIOS,
    const ROMIndex& parentROMs) {
  std::cout << "  <machine name=\"" << xmlEscape(machine.name) << "\"";
  if (!parent.empty()) {
    std::cout << " cloneof=\"" << xmlEscape(parent) << "\"";
  }
  if (!romOf.empty()) {
    std::cout << " romof=\"" << xmlEscape(romOf) << "\"";
  }
  if (isBIOS) {
    std::cout << " isbios=\"yes\"";
  }
  std::cout << ">\n";
  std::cout << "    <description>" << xmlEscape(machine.description) << "</description>\n";
  std::cout << "    <year>" << xmlEscape(machine.year) << "</year>\n";
  std::cout << "    <manufacturer>" << xmlEscape(machine.manufacturer) << "</manufacturer>\n";
  for (const ROM& rom : machine.roms) {
    std::cout << "    <rom name=\"" << xmlEscape(rom.name) << "\" size=\"" << rom.size << "\"";
    if (!parent.empty() && matchesParent(rom, parentROMs)) {
      std::cout << " merge=\"" << xmlEscape(rom.name) << "\"";
    }
    if (rom.crc == 0) {
      std::cout << " status=\"nodump\"";
    } else {
      std::cout << " crc=\"" << crc32(rom.crc) << "\"";
    }
    std::cout << "/>\n";
  }
  std::cout << "  </machine>\n";
}

int run(std::string_view coreID) {
  const std::vector<Machine> machines = enumerateMachines();
  std::map<std::string, const Machine*> byName;
  for (const Machine& machine : machines) {
    if (!byName.emplace(machine.name, &machine).second) {
      throw std::runtime_error("FBA2012_DAT_DUPLICATE_MACHINE");
    }
  }

  std::set<std::string> biosMachines;
  for (const Machine& machine : machines) {
    if (machine.boardROMMachine) {
      biosMachines.insert(machine.name);
    }
    if (!machine.boardROM.empty()) {
      if (!byName.contains(machine.boardROM)) {
        throw std::runtime_error("FBA2012_DAT_EXTERNAL_BOARD_ROM:" + machine.name + "->" + machine.boardROM);
      }
      biosMachines.insert(machine.boardROM);
    }
  }

  std::vector<std::string> normalizedParents;
  std::cout << "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n";
  std::cout << "<datafile>\n";
  std::cout << "  <header>\n";
  std::cout << "    <name>" << xmlEscape(coreID) << "</name>\n";
  std::cout << "    <description>Retrom locked " << xmlEscape(coreID) << " core DAT</description>\n";
  std::cout << "    <version>0.2.97.29</version>\n";
  std::cout << "  </header>\n";
  for (const Machine& machine : machines) {
    std::string parent = machine.parent;
    if (!parent.empty() && !byName.contains(parent)) {
      const bool allowed = coreID == "fbalpha2012_cps2" && machine.name == "mmancp2u" && parent == "megaman";
      if (!allowed) {
        throw std::runtime_error("FBA2012_DAT_EXTERNAL_PARENT:" + machine.name + "->" + parent);
      }
      normalizedParents.push_back(machine.name + "->" + parent);
      parent.clear();
    }
    std::string romOf = parent;
    if (!machine.boardROM.empty()) {
      romOf = machine.boardROM;
    }
    ROMIndex parentROMs;
    if (!parent.empty()) {
      parentROMs = indexROMs(*byName.at(parent));
    }
    writeMachine(machine, parent, romOf, biosMachines.contains(machine.name), parentROMs);
  }
  std::cout << "</datafile>\n";

  std::cerr << "RETROM_FBA2012_DAT_STATS={\"machineCount\":" << machines.size()
            << ",\"normalizedExternalParents\":[";
  for (std::size_t index = 0; index < normalizedParents.size(); ++index) {
    if (index != 0) {
      std::cerr << ',';
    }
    std::cerr << '"' << normalizedParents[index] << '"';
  }
  std::cerr << "],\"explicitBiosMachineCount\":" << biosMachines.size()
            << ",\"baseDependencyTargetCount\":" << biosMachines.size() << "}\n";
  return 0;
}

}  // namespace

int main(int argc, char** argv) {
  try {
    if (argc != 2) {
      throw std::runtime_error("usage: fbalpha2012-dat-enumerator CORE_ID");
    }
    const std::string_view coreID(argv[1]);
    if (coreID != "fbalpha2012_cps1" && coreID != "fbalpha2012_cps2") {
      throw std::runtime_error("FBA2012_DAT_UNSUPPORTED_CORE");
    }
    return run(coreID);
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
